package automation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/agent"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"
	_ "time/tzdata"
)

var (
	ErrInvalid      = errors.New("请检查自动任务的名称、任务内容、模型和触发时间")
	ErrNotFound     = errors.New("自动任务不存在")
	ErrConflict     = errors.New("自动任务已发生变化，请刷新后再试")
	ErrUnauthorized = errors.New("Webhook 凭证无效")
	ErrPaused       = errors.New("此自动任务已暂停")
	ErrLimit        = errors.New("自动任务或待处理事件过多，请稍后再试")
)

type Config struct {
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	ProjectID  uuid.UUID  `json:"project_id"`
	Agent      string     `json:"agent_slug"`
	ModelID    uuid.UUID  `json:"model_deployment_id"`
	Prompt     string     `json:"prompt"`
	Permission string     `json:"permission_mode"`
	Enabled    bool       `json:"enabled"`
	Cron       string     `json:"cron"`
	Timezone   string     `json:"timezone"`
	RunAt      *time.Time `json:"run_at"`
}
type Definition struct {
	Config
	ID          uuid.UUID  `json:"id"`
	WorkspaceID uuid.UUID  `json:"-"`
	UserID      uuid.UUID  `json:"created_by"`
	NextRunAt   *time.Time `json:"next_run_at"`
	Revision    int64      `json:"revision"`
	CreatedAt   time.Time  `json:"created_at"`
}
type Input struct {
	ID             uuid.UUID `json:"id"`
	WorkspaceID    uuid.UUID `json:"workspace_id"`
	UserID         uuid.UUID `json:"user_id"`
	ConversationID uuid.UUID `json:"conversation_id"`
	Config         Config    `json:"config"`
}
type Runner interface {
	Start(context.Context, Input) (uuid.UUID, error)
	Lookup(context.Context, Input) (uuid.UUID, error)
}
type Service struct {
	DB     *pgxpool.Pool
	Runner Runner
}

const columns = `id,workspace_id,created_by,name,kind,project_id,agent_slug,model_deployment_id,prompt,permission_mode,enabled,cron,timezone,run_at,next_run_at,revision,created_at`

type scanner interface{ Scan(...any) error }

func scanDefinition(row scanner) (d Definition, err error) {
	err = row.Scan(&d.ID, &d.WorkspaceID, &d.UserID, &d.Name, &d.Kind, &d.ProjectID, &d.Agent, &d.ModelID, &d.Prompt, &d.Permission, &d.Enabled, &d.Cron, &d.Timezone, &d.RunAt, &d.NextRunAt, &d.Revision, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return
}
func (s *Service) List(ctx context.Context, workspace, user uuid.UUID) ([]Definition, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+columns+` FROM automations WHERE workspace_id=$1 AND created_by=$2 AND deleted_at IS NULL ORDER BY created_at DESC,id DESC LIMIT 100`, workspace, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Definition{}
	for rows.Next() {
		d, err := scanDefinition(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}
func (s *Service) Get(ctx context.Context, workspace, user, id uuid.UUID) (Definition, error) {
	return scanDefinition(s.DB.QueryRow(ctx, `SELECT `+columns+` FROM automations WHERE workspace_id=$1 AND created_by=$2 AND id=$3 AND deleted_at IS NULL`, workspace, user, id))
}

func normalize(c Config) Config {
	c.Name = strings.TrimSpace(c.Name)
	c.Prompt = strings.TrimSpace(c.Prompt)
	c.Cron = strings.TrimSpace(c.Cron)
	if c.Agent == "" {
		c.Agent = "lester"
	}
	if c.Permission == "" {
		c.Permission = "standard"
	}
	if c.Timezone == "" {
		c.Timezone = "UTC"
	}
	return c
}
func nextOccurrence(c Config, after time.Time) (*time.Time, error) {
	if c.Kind != "schedule" {
		if c.Kind != "webhook" || c.Cron != "" || c.RunAt != nil {
			return nil, ErrInvalid
		}
		return nil, nil
	}
	location, err := time.LoadLocation(c.Timezone)
	if err != nil || c.Timezone == "Local" {
		return nil, ErrInvalid
	}
	if (c.Cron == "") == (c.RunAt == nil) {
		return nil, ErrInvalid
	}
	var next time.Time
	if c.RunAt != nil {
		next = c.RunAt.UTC()
		if c.Enabled && !next.After(after) {
			return nil, ErrInvalid
		}
	} else {
		if len(strings.Fields(c.Cron)) != 5 {
			return nil, ErrInvalid
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		schedule, err := parser.Parse(c.Cron)
		if err != nil {
			return nil, ErrInvalid
		}
		next = schedule.Next(after.In(location)).UTC()
		if next.IsZero() {
			return nil, ErrInvalid
		}
	}
	if !c.Enabled {
		return nil, nil
	}
	return &next, nil
}
func (s *Service) validate(ctx context.Context, workspace uuid.UUID, c Config) (*time.Time, error) {
	if len([]rune(c.Name)) < 1 || len([]rune(c.Name)) > 120 || len(c.Prompt) < 1 || len(c.Prompt) > 16000 || len(c.Cron) > 100 || len(c.Timezone) > 100 || (c.Permission != "standard" && c.Permission != "auto" && c.Permission != "full") {
		return nil, ErrInvalid
	}
	var valid bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE workspace_id=$1 AND id=$2) AND EXISTS(SELECT 1 FROM model_deployments WHERE id=$3 AND enabled AND workspace_id IN ($1,$4))`, workspace, c.ProjectID, c.ModelID, model.SystemWorkspaceID).Scan(&valid); err != nil {
		return nil, err
	}
	if !valid {
		return nil, ErrInvalid
	}
	if _, err := (&agent.Service{DB: s.DB}).Resolve(ctx, workspace, c.Agent); err != nil {
		return nil, ErrInvalid
	}
	return nextOccurrence(c, time.Now())
}
func secretToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := "lwh_" + base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}

// The client chooses a stable UUID for creation. A lost response can be read
// without creating a second enabled schedule or exposing an old secret.
func (s *Service) Save(ctx context.Context, workspace, user, id uuid.UUID, c Config, revision int64) (Definition, string, error) {
	c = normalize(c)
	next, err := s.validate(ctx, workspace, c)
	if err != nil {
		return Definition{}, "", err
	}
	if id == uuid.Nil || revision < 0 {
		return Definition{}, "", ErrInvalid
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Definition{}, "", err
	}
	defer tx.Rollback(ctx)
	var actor uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT u.id FROM users u JOIN workspace_members wm ON wm.user_id=u.id WHERE u.id=$1 AND wm.workspace_id=$2 AND NOT u.disabled AND (NOT u.email_verification_required OR u.email_verified) FOR UPDATE OF u`, user, workspace).Scan(&actor); err != nil {
		return Definition{}, "", ErrNotFound
	}
	secret := ""
	var hash []byte
	if revision == 0 {
		existing, lookupErr := scanDefinition(tx.QueryRow(ctx, `SELECT `+columns+` FROM automations WHERE id=$1 AND workspace_id=$2 AND created_by=$3 AND deleted_at IS NULL`, id, workspace, user))
		if lookupErr == nil {
			before, _ := json.Marshal(existing.Config)
			after, _ := json.Marshal(c)
			if string(before) == string(after) {
				return existing, "", nil
			}
			return Definition{}, "", ErrConflict
		} else if !errors.Is(lookupErr, ErrNotFound) {
			return Definition{}, "", lookupErr
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM automations WHERE workspace_id=$1 AND created_by=$2 AND deleted_at IS NULL`, workspace, user).Scan(&count); err != nil {
			return Definition{}, "", err
		}
		if count >= 100 {
			return Definition{}, "", ErrLimit
		}
		if c.Kind == "webhook" {
			secret, hash, err = secretToken()
			if err != nil {
				return Definition{}, "", err
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO automations(id,workspace_id,created_by,name,kind,project_id,agent_slug,model_deployment_id,prompt,permission_mode,enabled,cron,timezone,run_at,next_run_at,webhook_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, id, workspace, user, c.Name, c.Kind, c.ProjectID, c.Agent, c.ModelID, c.Prompt, c.Permission, c.Enabled, c.Cron, c.Timezone, c.RunAt, next, hash)
	} else {
		tag, updateErr := tx.Exec(ctx, `UPDATE automations SET name=$4,project_id=$5,agent_slug=$6,model_deployment_id=$7,prompt=$8,permission_mode=$9,enabled=$10,cron=$11,timezone=$12,run_at=$13,next_run_at=$14,revision=revision+1,updated_at=now() WHERE id=$1 AND workspace_id=$2 AND created_by=$3 AND deleted_at IS NULL AND revision=$15 AND kind=$16`, id, workspace, user, c.Name, c.ProjectID, c.Agent, c.ModelID, c.Prompt, c.Permission, c.Enabled, c.Cron, c.Timezone, c.RunAt, next, revision, c.Kind)
		err = updateErr
		if err == nil && tag.RowsAffected() != 1 {
			return Definition{}, "", ErrConflict
		}
	}
	if err != nil {
		return Definition{}, "", err
	}
	if !c.Enabled {
		if _, err = tx.Exec(ctx, `UPDATE automation_deliveries SET state='cancelled',error_code='paused',updated_at=now() WHERE workspace_id=$1 AND automation_id=$2 AND state='pending'`, workspace, id); err != nil {
			return Definition{}, "", err
		}
	}
	d, err := scanDefinition(tx.QueryRow(ctx, `SELECT `+columns+` FROM automations WHERE workspace_id=$1 AND id=$2`, workspace, id))
	if err != nil {
		return Definition{}, "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return Definition{}, "", err
	}
	return d, secret, nil
}
func (s *Service) Delete(ctx context.Context, workspace, user, id uuid.UUID, revision int64) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE automations SET deleted_at=now(),enabled=false,next_run_at=NULL,revision=revision+1,updated_at=now() WHERE workspace_id=$1 AND created_by=$2 AND id=$3 AND revision=$4 AND deleted_at IS NULL`, workspace, user, id, revision)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE automation_deliveries SET state='cancelled',error_code='deleted',updated_at=now() WHERE workspace_id=$1 AND automation_id=$2 AND state='pending'`, workspace, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Service) Rotate(ctx context.Context, workspace, user, id uuid.UUID, revision int64) (Definition, string, error) {
	token, hash, err := secretToken()
	if err != nil {
		return Definition{}, "", err
	}
	d, err := scanDefinition(s.DB.QueryRow(ctx, `UPDATE automations SET webhook_hash=$5,revision=revision+1,updated_at=now() WHERE workspace_id=$1 AND created_by=$2 AND id=$3 AND revision=$4 AND kind='webhook' AND deleted_at IS NULL RETURNING `+columns, workspace, user, id, revision, hash))
	if errors.Is(err, ErrNotFound) {
		err = ErrConflict
	}
	return d, token, err
}
