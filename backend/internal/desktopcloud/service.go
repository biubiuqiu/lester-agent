package desktopcloud

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func token() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	text := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(text))
	return text, sum[:], nil
}
func (s *Service) Register(ctx context.Context, p auth.Principal, id uuid.UUID, name, platform string, reauthorize bool) (Device, string, error) {
	name = strings.TrimSpace(name)
	if id == uuid.Nil || !utf8.ValidString(name) || len([]rune(name)) < 1 || len([]rune(name)) > 100 {
		return Device{}, "", errors.New("valid device id and name required")
	}
	if platform != "darwin" && platform != "win32" && platform != "linux" {
		return Device{}, "", errors.New("unsupported device platform")
	}
	raw, hash, err := token()
	if err != nil {
		return Device{}, "", err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Device{}, "", err
	}
	defer tx.Rollback(ctx)
	var user uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 AND NOT disabled AND (NOT email_verification_required OR email_verified) FOR UPDATE`, p.UserID).Scan(&user); err != nil {
		return Device{}, "", errDenied
	}
	var session uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE user_id=$1 AND token_hash=$2 AND expires_at>now() FOR UPDATE`, p.UserID, p.SessionHash).Scan(&session); err != nil {
		return Device{}, "", errDenied
	}
	var member bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=$1 AND user_id=$2)`, p.WorkspaceID, p.UserID).Scan(&member); err != nil || !member {
		return Device{}, "", errDenied
	}
	var old Device
	err = tx.QueryRow(ctx, `SELECT workspace_id,user_id,revoked_at IS NOT NULL FROM desktop_devices WHERE id=$1 FOR UPDATE`, id).Scan(&old.WorkspaceID, &old.UserID, &old.Revoked)
	if err == nil && (old.WorkspaceID != p.WorkspaceID || old.UserID != p.UserID) {
		return Device{}, "", errDenied
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Device{}, "", err
	}
	if old.Revoked && !reauthorize {
		return Device{}, "", errConflict
	}
	if err == nil && !reauthorize {
		var d Device
		d.ID = id
		d.WorkspaceID = p.WorkspaceID
		d.UserID = p.UserID
		err = tx.QueryRow(ctx, `UPDATE desktop_devices SET name=$2,platform=$3,updated_at=now() WHERE id=$1 RETURNING name,platform,generation,last_seen_at`, id, name, platform).Scan(&d.Name, &d.Platform, &d.Generation, &d.LastSeenAt)
		if err != nil {
			return Device{}, "", err
		}
		if err = tx.Commit(ctx); err != nil {
			return Device{}, "", err
		}
		d.Online = s.DesktopDeviceOnline(id)
		return d, "", nil
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM desktop_devices WHERE workspace_id=$1 AND user_id=$2 AND revoked_at IS NULL AND id<>$3`, p.WorkspaceID, p.UserID, id).Scan(&count); err != nil {
		return Device{}, "", err
	}
	if count >= 32 {
		return Device{}, "", errors.New("desktop device limit reached")
	}
	d := Device{ID: id, WorkspaceID: p.WorkspaceID, UserID: p.UserID, SessionID: &session, Name: name, Platform: platform}
	err = tx.QueryRow(ctx, `INSERT INTO desktop_devices(id,workspace_id,user_id,session_id,name,platform,token_hash) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO UPDATE SET session_id=$4,name=$5,platform=$6,token_hash=$7,generation=desktop_devices.generation+1,revoked_at=NULL,token_expires_at=now()+interval '30 days',updated_at=now() WHERE desktop_devices.workspace_id=EXCLUDED.workspace_id AND desktop_devices.user_id=EXCLUDED.user_id RETURNING generation,last_seen_at`, id, p.WorkspaceID, p.UserID, session, name, platform, hash).Scan(&d.Generation, &d.LastSeenAt)
	if err != nil {
		return Device{}, "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return Device{}, "", err
	}
	s.disconnect(id)
	return d, raw, nil
}

func (s *Service) ListDevices(ctx context.Context, p auth.Principal) ([]Device, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,name,platform,generation,revoked_at IS NOT NULL,last_seen_at FROM desktop_devices WHERE workspace_id=$1 AND user_id=$2 ORDER BY created_at DESC LIMIT 64`, p.WorkspaceID, p.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Device{}
	for rows.Next() {
		var d Device
		d.WorkspaceID = p.WorkspaceID
		d.UserID = p.UserID
		if err = rows.Scan(&d.ID, &d.Name, &d.Platform, &d.Generation, &d.Revoked, &d.LastSeenAt); err != nil {
			return nil, err
		}
		d.Online = !d.Revoked && s.DesktopDeviceOnline(d.ID)
		items = append(items, d)
	}
	return items, rows.Err()
}

func (s *Service) Revoke(ctx context.Context, p auth.Principal, id uuid.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE desktop_devices SET revoked_at=COALESCE(revoked_at,now()),generation=generation+1,updated_at=now() WHERE id=$1 AND workspace_id=$2 AND user_id=$3`, id, p.WorkspaceID, p.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errDenied
	}
	if _, err = tx.Exec(ctx, `UPDATE model_deployments SET enabled=false,is_default=false WHERE id IN(SELECT deployment_id FROM desktop_device_models WHERE device_id=$1)`, id); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.disconnect(id)
	return nil
}

func (s *Service) Rotate(ctx context.Context, p auth.Principal, id uuid.UUID) (string, error) {
	raw, hash, err := token()
	if err != nil {
		return "", err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var user, session uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 AND NOT disabled AND (NOT email_verification_required OR email_verified) FOR UPDATE`, p.UserID).Scan(&user); err != nil {
		return "", errDenied
	}
	if err = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE user_id=$1 AND token_hash=$2 AND expires_at>now() FOR UPDATE`, p.UserID, p.SessionHash).Scan(&session); err != nil {
		return "", errDenied
	}
	tag, err := tx.Exec(ctx, `UPDATE desktop_devices SET token_hash=$4,session_id=$5,generation=generation+1,token_expires_at=now()+interval '30 days',updated_at=now() WHERE id=$1 AND workspace_id=$2 AND user_id=$3 AND revoked_at IS NULL AND EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=$2 AND user_id=$3)`, id, p.WorkspaceID, p.UserID, hash, session)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() != 1 {
		return "", errDenied
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	s.disconnect(id)
	return raw, nil
}

func (s *Service) deviceFromToken(ctx context.Context, raw string) (Device, auth.Principal, []byte, error) {
	if len(raw) != 43 {
		return Device{}, auth.Principal{}, nil, errDenied
	}
	sum := sha256.Sum256([]byte(raw))
	var d Device
	var p auth.Principal
	err := s.DB.QueryRow(ctx, `SELECT d.id,d.workspace_id,d.user_id,d.name,d.platform,d.generation,d.last_seen_at,s.id,s.token_hash,s.expires_at,u.email,u.display_name FROM desktop_devices d JOIN sessions s ON s.id=d.session_id JOIN users u ON u.id=d.user_id WHERE d.token_hash=$1 AND d.revoked_at IS NULL AND s.user_id=d.user_id AND s.expires_at>now() AND d.token_expires_at>now() AND NOT u.disabled AND (NOT u.email_verification_required OR u.email_verified) AND EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=d.workspace_id AND user_id=d.user_id)`, sum[:]).Scan(&d.ID, &d.WorkspaceID, &d.UserID, &d.Name, &d.Platform, &d.Generation, &d.LastSeenAt, &d.SessionID, &p.SessionHash, &p.RefreshExpires, &p.Email, &p.DisplayName)
	if err != nil {
		return Device{}, auth.Principal{}, nil, errDenied
	}
	p.UserID = d.UserID
	p.WorkspaceID = d.WorkspaceID
	return d, p, sum[:], nil
}

func (s *Service) liveDevice(ctx context.Context, p auth.Principal, id uuid.UUID) (Device, error) {
	var d Device
	err := s.DB.QueryRow(ctx, `SELECT d.id,d.workspace_id,d.user_id,d.name,d.platform,d.generation,d.last_seen_at FROM desktop_devices d JOIN sessions s ON s.id=d.session_id JOIN users u ON u.id=d.user_id WHERE d.id=$1 AND d.workspace_id=$2 AND d.user_id=$3 AND d.revoked_at IS NULL AND s.expires_at>now() AND d.token_expires_at>now() AND NOT u.disabled AND (NOT u.email_verification_required OR u.email_verified) AND EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=d.workspace_id AND user_id=d.user_id)`, id, p.WorkspaceID, p.UserID).Scan(&d.ID, &d.WorkspaceID, &d.UserID, &d.Name, &d.Platform, &d.Generation, &d.LastSeenAt)
	if err != nil {
		return Device{}, errDenied
	}
	return d, nil
}

func (s *Service) SyncModels(ctx context.Context, p auth.Principal, id uuid.UUID, items []ModelMetadata) error {
	if len(items) > 64 {
		return errors.New("at most 64 local models can be shared")
	}
	keys := map[string]bool{}
	for _, item := range items {
		if !validKey(item.ID) || keys[item.ID] || strings.TrimSpace(item.Name) == "" || len([]rune(item.Name)) > 120 || len(item.Provider) > 64 || len(item.ModelID) > 240 || !utf8.ValidString(item.Name) || !utf8.ValidString(item.Provider) || !utf8.ValidString(item.ModelID) || item.ContextWindow < 0 || item.OutputReserve < 0 || (item.ContextWindow > 0 && item.OutputReserve >= item.ContextWindow) || item.ContextWindow > 2147483647 || item.OutputReserve > 2147483647 || (item.Source != "" && item.Source != "local") {
			return errors.New("invalid local model metadata")
		}
		keys[item.ID] = true
	}
	d, err := s.liveDevice(ctx, p, id)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = s.lockDevice(ctx, tx, p, id); err != nil {
		return err
	}
	var generation int64
	if err = tx.QueryRow(ctx, `SELECT generation FROM desktop_devices WHERE id=$1 AND user_id=$2 AND workspace_id=$3 AND revoked_at IS NULL FOR UPDATE`, id, p.UserID, p.WorkspaceID).Scan(&generation); err != nil || generation != d.Generation {
		return errConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE model_deployments SET enabled=false,is_default=false WHERE id IN(SELECT deployment_id FROM desktop_device_models WHERE device_id=$1)`, id); err != nil {
		return err
	}
	for _, item := range items {
		var connection, deployment uuid.UUID
		err = tx.QueryRow(ctx, `SELECT connection_id,deployment_id FROM desktop_device_models WHERE device_id=$1 AND model_key=$2`, id, item.ID).Scan(&connection, &deployment)
		config, _ := json.Marshal(map[string]any{"desktop_device_id": id.String(), "desktop_owner_id": p.UserID.String(), "desktop_model_key": item.ID, "desktop_device_name": d.Name, "local_provider": item.Provider, "local_model_id": item.ModelID})
		caps := []byte(`{"Streaming":true,"Tools":true,"Vision":true}`)
		if errors.Is(err, pgx.ErrNoRows) {
			if err = tx.QueryRow(ctx, `INSERT INTO model_connections(workspace_id,name,provider,protocol,config) VALUES($1,$2,'desktop','desktop',$3) RETURNING id`, p.WorkspaceID, d.Name+" · "+item.Name, config).Scan(&connection); err != nil {
				return err
			}
			if err = tx.QueryRow(ctx, `INSERT INTO model_deployments(workspace_id,connection_id,name,model_id,capabilities,context_window,output_reserve) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, p.WorkspaceID, connection, item.Name, item.ID, caps, item.ContextWindow, item.OutputReserve).Scan(&deployment); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO desktop_device_models(device_id,model_key,connection_id,deployment_id) VALUES($1,$2,$3,$4)`, id, item.ID, connection, deployment); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if _, err = tx.Exec(ctx, `UPDATE model_connections SET name=$2,config=$3 WHERE id=$1`, connection, d.Name+" · "+item.Name, config); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE model_deployments SET name=$2,enabled=true,model_id=$3,context_window=$4,output_reserve=$5 WHERE id=$1`, deployment, item.Name, item.ID, item.ContextWindow, item.OutputReserve); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}
func validKey(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:", r)) {
			return false
		}
	}
	return true
}

func (s *Service) SyncConversations(ctx context.Context, p auth.Principal, id uuid.UUID, items []Conversation) error {
	if len(items) > 200 {
		return errors.New("at most 200 conversation updates per batch")
	}
	d, err := s.liveDevice(ctx, p, id)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = s.lockDevice(ctx, tx, p, id); err != nil {
		return err
	}
	var generation int64
	if err = tx.QueryRow(ctx, `SELECT generation FROM desktop_devices WHERE id=$1 AND revoked_at IS NULL FOR UPDATE`, id).Scan(&generation); err != nil || generation != d.Generation {
		return errConflict
	}
	for _, item := range items {
		item.UpdatedAt = item.UpdatedAt.UTC().Truncate(time.Microsecond)
		if item.ID == uuid.Nil || item.Revision < 1 || item.UpdatedAt.IsZero() || item.UpdatedAt.After(time.Now().Add(5*time.Minute)) || len([]rune(item.ProjectName)) > 120 || len([]rune(item.Title)) < 1 || len([]rune(item.Title)) > 240 || !validStatus(item.Status) {
			return errors.New("invalid local conversation metadata")
		}
		var oldDevice uuid.UUID
		var revision int64
		var oldRaw []byte
		var oldTime time.Time
		err = tx.QueryRow(ctx, `SELECT device_id,revision,jsonb_build_object('project_id',project_id,'project_name',project_name,'title',title,'status',status,'updated_at',updated_at),updated_at FROM desktop_conversations WHERE id=$1 FOR UPDATE`, item.ID).Scan(&oldDevice, &revision, &oldRaw, &oldTime)
		if err == nil {
			if oldDevice != id || item.Revision < revision {
				return errConflict
			}
			if item.Revision == revision {
				var old map[string]any
				_ = json.Unmarshal(oldRaw, &old)
				project := ""
				if item.ProjectID != nil {
					project = item.ProjectID.String()
				}
				oldProject, _ := old["project_id"].(string)
				if oldProject != project || old["project_name"] != item.ProjectName || old["title"] != item.Title || old["status"] != item.Status || !oldTime.Equal(item.UpdatedAt) {
					return errConflict
				}
				continue
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if tag, writeErr := tx.Exec(ctx, `INSERT INTO desktop_conversations(id,device_id,workspace_id,user_id,project_id,project_name,title,status,revision,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO UPDATE SET project_id=$5,project_name=$6,title=$7,status=$8,revision=$9,updated_at=$10,synced_at=now() WHERE desktop_conversations.device_id=EXCLUDED.device_id AND desktop_conversations.revision<EXCLUDED.revision`, item.ID, id, p.WorkspaceID, p.UserID, item.ProjectID, item.ProjectName, item.Title, item.Status, item.Revision, item.UpdatedAt); writeErr != nil {
			return writeErr
		} else if tag.RowsAffected() != 1 {
			return errConflict
		}
	}
	return tx.Commit(ctx)
}
func validStatus(status string) bool {
	switch status {
	case "idle", "running", "waiting", "completed", "failed", "cancelled", "interrupted", "unknown":
		return true
	}
	return false
}

func (s *Service) ListConversations(ctx context.Context, p auth.Principal) ([]Conversation, error) {
	items := []Conversation{}
	rows, err := s.DB.Query(ctx, `SELECT c.id,c.project_id,COALESCE(p.name,''),c.title,COALESCE(latest.status,'idle'),c.updated_at FROM conversations c LEFT JOIN LATERAL(SELECT status FROM runs WHERE conversation_id=c.id ORDER BY created_at DESC,id DESC LIMIT 1) latest ON true LEFT JOIN projects p ON p.id=c.project_id AND p.workspace_id=c.workspace_id WHERE c.workspace_id=$1 AND c.created_by=$2 ORDER BY c.updated_at DESC,c.id DESC LIMIT 200`, p.WorkspaceID, p.UserID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c Conversation
		if err = rows.Scan(&c.ID, &c.ProjectID, &c.ProjectName, &c.Title, &c.Status, &c.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		c.Source = "cloud"
		c.Online = true
		items = append(items, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = s.DB.Query(ctx, `SELECT c.id,c.project_id,c.project_name,c.title,c.status,c.revision,c.updated_at,d.id,d.name,d.last_seen_at,d.revoked_at IS NOT NULL FROM desktop_conversations c JOIN desktop_devices d ON d.id=c.device_id WHERE c.workspace_id=$1 AND c.user_id=$2 ORDER BY c.updated_at DESC,c.id DESC LIMIT 200`, p.WorkspaceID, p.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Conversation
		var revoked bool
		if err = rows.Scan(&c.ID, &c.ProjectID, &c.ProjectName, &c.Title, &c.Status, &c.Revision, &c.UpdatedAt, &c.DeviceID, &c.DeviceName, &c.LastSeenAt, &revoked); err != nil {
			return nil, err
		}
		c.Source = "local"
		c.Online = !revoked && s.DesktopDeviceOnline(c.DeviceID)
		items = append(items, c)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	if len(items) > 200 {
		items = items[:200]
	}
	return items, nil
}

// Mutations serialize in the same user -> session -> device order as account
// revocation. A previously authenticated request cannot revive a revoked peer.
func (s *Service) lockDevice(ctx context.Context, tx pgx.Tx, p auth.Principal, id uuid.UUID) error {
	var user, session uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 AND NOT disabled AND (NOT email_verification_required OR email_verified) FOR UPDATE`, p.UserID).Scan(&user); err != nil {
		return errDenied
	}
	if err := tx.QueryRow(ctx, `SELECT ss.id FROM sessions ss JOIN desktop_devices d ON d.session_id=ss.id WHERE d.id=$1 AND d.workspace_id=$2 AND d.user_id=$3 AND d.revoked_at IS NULL AND d.token_expires_at>now() AND ss.expires_at>now() AND EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=$2 AND user_id=$3) FOR UPDATE OF ss`, id, p.WorkspaceID, p.UserID).Scan(&session); err != nil {
		return errDenied
	}
	return nil
}
