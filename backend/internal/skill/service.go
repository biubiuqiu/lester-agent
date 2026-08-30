package skill

import (
	"archive/zip"
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/blob"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxBundleSize    = 2 << 20
	maxBundleFiles   = 32
	maxExtractedSize = 4 << 20
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

//go:embed defaults/*/SKILL.md
var defaultFiles embed.FS

type Skill struct {
	ID          uuid.UUID  `json:"id"`
	Slug        string     `json:"slug"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Version     string     `json:"version"`
	Source      string     `json:"source"`
	SizeBytes   int64      `json:"size_bytes"`
	InstalledAt *time.Time `json:"installed_at,omitempty"`
}

type Service struct {
	db        *pgxpool.Pool
	objects   blob.Store
	sandboxes *sandbox.Client
}

type defaultSkill struct {
	Slug, Name, Description, Version string
}

var defaults = []defaultSkill{
	{Slug: "code-review", Name: "Code Review", Description: "检查代码正确性、回归风险、安全问题和测试缺口。", Version: "1.0.0"},
	{Slug: "project-planner", Name: "Project Planner", Description: "把产品或工程目标拆成可验证、依赖清晰的执行计划。", Version: "1.0.0"},
	{Slug: "data-explorer", Name: "Data Explorer", Description: "安全探索本地结构化数据，并给出可复现的证据和结论。", Version: "1.0.0"},
}

func New(db *pgxpool.Pool, objects blob.Store, sandboxes *sandbox.Client) *Service {
	return &Service{db: db, objects: objects, sandboxes: sandboxes}
}

func (s *Service) SeedDefaults(ctx context.Context) error {
	for _, item := range defaults {
		content, err := defaultFiles.ReadFile("defaults/" + item.Slug + "/SKILL.md")
		if err != nil {
			return err
		}
		bundle, err := zipBundle(map[string][]byte{"SKILL.md": content})
		if err != nil {
			return err
		}
		key := fmt.Sprintf("skills/builtin/%s/%s.zip", item.Slug, item.Version)
		if err = s.objects.Put(ctx, key, bytes.NewReader(bundle), int64(len(bundle)), "application/zip"); err != nil {
			return fmt.Errorf("store default skill %s: %w", item.Slug, err)
		}
		_, err = s.db.Exec(ctx, `INSERT INTO skills(slug,name,description,version,object_key,source,size_bytes)
			VALUES($1,$2,$3,$4,$5,'builtin',$6)
			ON CONFLICT(slug) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,version=EXCLUDED.version,object_key=EXCLUDED.object_key,size_bytes=EXCLUDED.size_bytes,updated_at=now()`, item.Slug, item.Name, item.Description, item.Version, key, len(bundle))
		if err != nil {
			return fmt.Errorf("catalog default skill %s: %w", item.Slug, err)
		}
	}
	return nil
}

func (s *Service) List(ctx context.Context) ([]Skill, error) {
	rows, err := s.db.Query(ctx, `SELECT id,slug,name,description,version,source,size_bytes FROM skills ORDER BY name,slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Skill{}
	for rows.Next() {
		var item Skill
		if err = rows.Scan(&item.ID, &item.Slug, &item.Name, &item.Description, &item.Version, &item.Source, &item.SizeBytes); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) Installed(ctx context.Context, workspaceID, conversationID uuid.UUID) ([]Skill, error) {
	rows, err := s.db.Query(ctx, `SELECT s.id,s.slug,s.name,s.description,s.version,s.source,s.size_bytes,cs.installed_at
		FROM conversation_skills cs JOIN skills s ON s.id=cs.skill_id JOIN conversations c ON c.id=cs.conversation_id
		WHERE cs.conversation_id=$2 AND c.workspace_id=$1 ORDER BY s.name,s.slug`, workspaceID, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Skill{}
	for rows.Next() {
		var item Skill
		var installedAt time.Time
		if err = rows.Scan(&item.ID, &item.Slug, &item.Name, &item.Description, &item.Version, &item.Source, &item.SizeBytes, &installedAt); err != nil {
			return nil, err
		}
		item.InstalledAt = &installedAt
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) Install(ctx context.Context, workspaceID, userID, conversationID uuid.UUID, sandboxID, workDir, slug string) (Skill, error) {
	if !slugPattern.MatchString(slug) {
		return Skill{}, errors.New("invalid skill slug")
	}
	var item Skill
	var objectKey string
	err := s.db.QueryRow(ctx, `SELECT s.id,s.slug,s.name,s.description,s.version,s.source,s.size_bytes,s.object_key
		FROM skills s WHERE s.slug=$1 AND EXISTS(SELECT 1 FROM conversations c WHERE c.id=$2 AND c.workspace_id=$3)`, slug, conversationID, workspaceID).
		Scan(&item.ID, &item.Slug, &item.Name, &item.Description, &item.Version, &item.Source, &item.SizeBytes, &objectKey)
	if err != nil {
		return Skill{}, err
	}
	reader, err := s.objects.Get(ctx, objectKey)
	if err != nil {
		return Skill{}, err
	}
	defer reader.Close()
	bundle, err := io.ReadAll(io.LimitReader(reader, maxBundleSize+1))
	if err != nil {
		return Skill{}, err
	}
	if len(bundle) > maxBundleSize {
		return Skill{}, errors.New("skill bundle exceeds the 2 MiB limit")
	}
	files, err := unzipBundle(bundle)
	if err != nil {
		return Skill{}, err
	}
	if _, exists := files["SKILL.md"]; !exists {
		return Skill{}, errors.New("skill bundle does not contain SKILL.md")
	}
	for name, content := range files {
		target := path.Join(".agent/skills", slug, name)
		if err = s.sandboxes.WriteFile(ctx, sandboxID, workDir, target, content); err != nil {
			return Skill{}, fmt.Errorf("install %s: %w", target, err)
		}
	}
	var installedAt time.Time
	if err = s.db.QueryRow(ctx, `INSERT INTO conversation_skills(conversation_id,skill_id,installed_by) VALUES($1,$2,$3)
		ON CONFLICT(conversation_id,skill_id) DO UPDATE SET installed_by=EXCLUDED.installed_by,installed_at=now() RETURNING installed_at`, conversationID, item.ID, userID).Scan(&installedAt); err != nil {
		return Skill{}, err
	}
	item.InstalledAt = &installedAt
	return item, nil
}

func (s *Service) Uninstall(ctx context.Context, workspaceID, conversationID uuid.UUID, sandboxID, workDir, slug string) error {
	if !slugPattern.MatchString(slug) {
		return errors.New("invalid skill slug")
	}
	var skillID uuid.UUID
	if err := s.db.QueryRow(ctx, `SELECT s.id FROM skills s JOIN conversation_skills cs ON cs.skill_id=s.id JOIN conversations c ON c.id=cs.conversation_id WHERE s.slug=$1 AND cs.conversation_id=$2 AND c.workspace_id=$3`, slug, conversationID, workspaceID).Scan(&skillID); err != nil {
		return err
	}
	result, err := s.sandboxes.Exec(ctx, sandboxID, sandbox.Command{Command: "rm -rf -- " + shellQuote(path.Join(".agent/skills", slug)), WorkDir: workDir, TimeoutSeconds: 30})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("remove skill files: %s", strings.TrimSpace(result.Stderr))
	}
	_, err = s.db.Exec(ctx, `DELETE FROM conversation_skills WHERE conversation_id=$1 AND skill_id=$2`, conversationID, skillID)
	return err
}

func zipBundle(files map[string][]byte) ([]byte, error) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry, err := archive.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = entry.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func unzipBundle(bundle []byte) (map[string][]byte, error) {
	archive, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		return nil, err
	}
	if len(archive.File) == 0 || len(archive.File) > maxBundleFiles {
		return nil, fmt.Errorf("skill bundle must contain between 1 and %d files", maxBundleFiles)
	}
	files := make(map[string][]byte, len(archive.File))
	extractedSize := 0
	for _, entry := range archive.File {
		clean := path.Clean(strings.ReplaceAll(entry.Name, "\\", "/"))
		if entry.FileInfo().IsDir() {
			continue
		}
		if clean == "." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || entry.UncompressedSize64 > maxBundleSize {
			return nil, fmt.Errorf("unsafe skill bundle entry %q", entry.Name)
		}
		reader, err := entry.Open()
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(io.LimitReader(reader, maxBundleSize+1))
		_ = reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if len(content) > maxBundleSize {
			return nil, fmt.Errorf("skill file %q is too large", entry.Name)
		}
		extractedSize += len(content)
		if extractedSize > maxExtractedSize {
			return nil, errors.New("skill bundle exceeds the 4 MiB extracted-size limit")
		}
		files[clean] = content
	}
	return files, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
