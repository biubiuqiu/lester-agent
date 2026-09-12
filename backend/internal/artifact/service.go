package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/biubiuqiu/lester-agent/backend/internal/blob"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Asset struct {
	Key         string `json:"key"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	Hash        string `json:"hash"`
}
type Artifact struct {
	ID             uuid.UUID `json:"id"`
	ConversationID uuid.UUID `json:"conversation_id"`
	ProjectID      uuid.UUID `json:"project_id"`
	Name           string    `json:"name"`
	SourcePath     string    `json:"source_path"`
	EntryPath      string    `json:"entry_path"`
	Version        uuid.UUID `json:"version"`
	Status         string    `json:"status"`
	URL            string    `json:"url"`
	FileCount      int       `json:"file_count"`
	SizeBytes      int64     `json:"size_bytes"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Warnings       []string  `json:"warnings,omitempty"`
}
type PublishInput struct {
	Name       string    `json:"name"`
	SourcePath string    `json:"source_path"`
	Entry      string    `json:"entry,omitempty"`
	ArtifactID uuid.UUID `json:"artifact_id,omitempty"`
}
type Service struct {
	DB      *pgxpool.Pool
	Store   blob.Store
	Files   Files
	BaseURL string
	Prepare func(context.Context, uuid.UUID, uuid.UUID) (sandboxID, workDir string, err error)
}

func ValidateOrigin(base, app string) error {
	u, e := url.Parse(base)
	a, ae := url.Parse(app)
	if e != nil || ae != nil || u.Host == "" || a.Host == "" || (a.Scheme != "http" && a.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("ARTIFACT_PUBLIC_URL must be an HTTP(S) origin")
	}
	if strings.EqualFold(u.Hostname(), a.Hostname()) {
		return errors.New("artifact hosting must use a different hostname from WEB_ORIGIN (use 127.0.0.1 for local hosting and localhost for the app)")
	}
	return nil
}
func (s *Service) link(id uuid.UUID) string {
	return strings.TrimRight(s.BaseURL, "/") + "/s/" + id.String() + "/"
}
func (s *Service) List(ctx context.Context, workspace uuid.UUID) ([]Artifact, error) {
	rows, err := s.DB.Query(ctx, `SELECT a.id,a.conversation_id,c.project_id,a.name,a.source_path,a.entry_path,a.version,a.status,a.file_count,a.size_bytes,a.created_at,a.updated_at FROM artifacts a JOIN conversations c ON c.id=a.conversation_id AND c.workspace_id=a.workspace_id WHERE a.workspace_id=$1 ORDER BY a.created_at DESC,a.id LIMIT 1000`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Artifact{}
	for rows.Next() {
		var a Artifact
		if err = rows.Scan(&a.ID, &a.ConversationID, &a.ProjectID, &a.Name, &a.SourcePath, &a.EntryPath, &a.Version, &a.Status, &a.FileCount, &a.SizeBytes, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.URL = s.link(a.ID)
		items = append(items, a)
	}
	return items, rows.Err()
}
func (s *Service) Unpublish(ctx context.Context, workspace, id uuid.UUID) error {
	tag, err := s.DB.Exec(ctx, `UPDATE artifacts SET status='unpublished',updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspace, id)
	if err == nil && tag.RowsAffected() == 0 {
		return errors.New("产物不存在")
	}
	return err
}

// Publish stages all objects before atomically switching the public manifest.
// Existing deployments remain available if bundling/uploading fails. A bounded
// advisory lock serializes redeploy and unpublish for a single artifact.
func (s *Service) Publish(ctx context.Context, workspace, conversation uuid.UUID, in PublishInput) (Artifact, error) {
	var result Artifact
	if s.BaseURL == "" {
		return result, errors.New("尚未配置站点访问地址")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		in.Name = "HTML 站点"
	}
	if utf8.RuneCountInString(in.Name) > 120 {
		return result, errors.New("产物名称最多 120 个字符")
	}
	if strings.TrimSpace(in.SourcePath) == "" {
		return result, errors.New("请指定 HTML 文件或站点目录")
	}
	var project uuid.UUID
	if err := s.DB.QueryRow(ctx, `SELECT project_id FROM conversations WHERE workspace_id=$1 AND id=$2`, workspace, conversation).Scan(&project); err != nil {
		return result, errors.New("会话不存在")
	}
	id := in.ArtifactID
	if id == uuid.Nil {
		id = uuid.New()
	}
	// A database transaction holds only deployment metadata, never sandbox state.
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "artifact:"+id.String()); err != nil {
		return result, err
	}
	var old map[string]Asset
	if in.ArtifactID != uuid.Nil {
		var raw []byte
		if err = tx.QueryRow(ctx, `SELECT manifest FROM artifacts WHERE workspace_id=$1 AND id=$2 AND conversation_id=$3 FOR UPDATE`, workspace, id, conversation).Scan(&raw); err != nil {
			return result, errors.New("产物不存在或不属于当前会话")
		}
		if err = json.Unmarshal(raw, &old); err != nil {
			return result, err
		}
	}
	sid, workDir, err := s.Prepare(ctx, workspace, conversation)
	if err != nil {
		return result, fmt.Errorf("无法准备 Computer: %w", err)
	}
	bundle, err := Build(ctx, s.Files, sid, workDir, in.SourcePath, in.Entry, "/s/"+id.String()+"/")
	if err != nil {
		return result, err
	}
	version := uuid.New()
	manifest := map[string]Asset{}
	uploaded := []string{}
	committed := false
	defer func() {
		if !committed {
			s.remove(uploaded)
		}
	}()
	names := make([]string, 0, len(bundle.Files))
	for name := range bundle.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := bundle.Files[name]
		key := "artifacts/" + workspace.String() + "/" + id.String() + "/" + version.String() + "/" + name
		uploaded = append(uploaded, key)
		if err = s.Store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), ContentType(name)); err != nil {
			return result, errors.New("产物上传失败，原部署保持不变")
		}
		sum := sha256.Sum256(data)
		manifest[name] = Asset{Key: key, Size: int64(len(data)), ContentType: ContentType(name), Hash: hex.EncodeToString(sum[:])}
		result.SizeBytes += int64(len(data))
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return result, err
	}
	if in.ArtifactID == uuid.Nil {
		err = tx.QueryRow(ctx, `INSERT INTO artifacts(id,workspace_id,conversation_id,name,source_path,entry_path,version,manifest,file_count,size_bytes) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING created_at,updated_at`, id, workspace, conversation, in.Name, in.SourcePath, bundle.Entry, version, raw, len(manifest), result.SizeBytes).Scan(&result.CreatedAt, &result.UpdatedAt)
	} else {
		err = tx.QueryRow(ctx, `UPDATE artifacts SET name=$3,source_path=$4,entry_path=$5,version=$6,manifest=$7,file_count=$8,size_bytes=$9,status='published',updated_at=now() WHERE workspace_id=$1 AND id=$2 RETURNING created_at,updated_at`, workspace, id, in.Name, in.SourcePath, bundle.Entry, version, raw, len(manifest), result.SizeBytes).Scan(&result.CreatedAt, &result.UpdatedAt)
	}
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		// A lost commit acknowledgement has an unknown outcome. Retain staged
		// objects rather than deleting a potentially committed publication.
		committed = true
		return result, err
	}
	committed = true
	result.ID = id
	result.ConversationID = conversation
	result.ProjectID = project
	result.Name = in.Name
	result.SourcePath = in.SourcePath
	result.EntryPath = bundle.Entry
	result.Version = version
	result.Status = "published"
	result.FileCount = len(manifest)
	result.URL = s.link(id)
	result.Warnings = bundle.Warnings
	// Old objects are retained: in-flight requests can finish after a deployment
	// switch. Public reads always consult the current manifest and publication state.
	return result, nil
}
func (s *Service) remove(keys []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, key := range keys {
		if err := s.Store.Delete(ctx, key); err != nil {
			slog.Warn("artifact staging cleanup failed", "error", err)
			return
		}
	}
}

type Published struct {
	Entry     string
	Manifest  map[string]Asset
	UpdatedAt time.Time
}
type Catalog interface {
	Published(context.Context, uuid.UUID) (Published, error)
}

func (s *Service) Published(ctx context.Context, id uuid.UUID) (Published, error) {
	var p Published
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT entry_path,manifest,updated_at FROM artifacts WHERE id=$1 AND status='published'`, id).Scan(&p.Entry, &raw, &p.UpdatedAt)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(raw, &p.Manifest)
	return p, err
}
