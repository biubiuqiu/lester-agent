package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/biubiuqiu/lester-agent/backend/internal/secret"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var providers = map[string]string{"openai": "openai", "azure_openai": "openai", "openai_compatible": "openai", "anthropic": "anthropic", "bedrock": "anthropic", "vertex": "anthropic", "foundry": "anthropic", "anthropic_compatible": "anthropic"}

type Connection struct {
	ID           uuid.UUID      `json:"id"`
	WorkspaceID  uuid.UUID      `json:"workspace_id"`
	Name         string         `json:"name"`
	Provider     string         `json:"provider"`
	Protocol     string         `json:"protocol"`
	Endpoint     string         `json:"endpoint"`
	Config       map[string]any `json:"config"`
	CredentialID uuid.UUID      `json:"-"`
}
type Deployment struct {
	ID           uuid.UUID         `json:"id"`
	ConnectionID uuid.UUID         `json:"connection_id"`
	Name         string            `json:"name"`
	ModelID      string            `json:"model_id"`
	IsDefault    bool              `json:"is_default"`
	Capabilities ModelCapabilities `json:"capabilities"`
}
type Store struct {
	db      *pgxpool.Pool
	secrets *secret.Store
}

func NewStore(db *pgxpool.Pool, secrets *secret.Store) *Store {
	return &Store{db: db, secrets: secrets}
}
func (s *Store) CreateConnection(ctx context.Context, workspaceID uuid.UUID, name, provider, endpoint string, config map[string]any, credential string) (Connection, error) {
	protocol, ok := providers[provider]
	if !ok {
		return Connection{}, errors.New("unsupported provider")
	}
	if endpoint == "" {
		endpoint = defaultEndpoint(provider, config)
	}
	credentialID, err := s.secrets.Put(ctx, workspaceID, []byte(credential))
	if err != nil {
		return Connection{}, err
	}
	raw, _ := json.Marshal(config)
	var result Connection
	result.Config = config
	result.CredentialID = credentialID
	err = s.db.QueryRow(ctx, `INSERT INTO model_connections(workspace_id,name,provider,protocol,endpoint,config,credential_id) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id,workspace_id,name,provider,protocol,endpoint`, workspaceID, name, provider, protocol, endpoint, raw, credentialID).Scan(&result.ID, &result.WorkspaceID, &result.Name, &result.Provider, &result.Protocol, &result.Endpoint)
	return result, err
}
func (s *Store) ListConnections(ctx context.Context, workspaceID uuid.UUID) ([]Connection, error) {
	rows, err := s.db.Query(ctx, `SELECT id,workspace_id,name,provider,protocol,COALESCE(endpoint,''),config,credential_id FROM model_connections WHERE workspace_id=$1 ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Connection{}
	for rows.Next() {
		var item Connection
		var raw []byte
		if err = rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Provider, &item.Protocol, &item.Endpoint, &raw, &item.CredentialID); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &item.Config)
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) CreateDeployment(ctx context.Context, workspaceID, connectionID uuid.UUID, name, modelID string, isDefault bool) (Deployment, error) {
	if isDefault {
		_, _ = s.db.Exec(ctx, `UPDATE model_deployments SET is_default=false WHERE workspace_id=$1`, workspaceID)
	}
	caps := ModelCapabilities{Streaming: true, Tools: true, Vision: true, StructuredOutput: true, TokenCounting: true}
	raw, _ := json.Marshal(caps)
	var d Deployment
	d.Capabilities = caps
	err := s.db.QueryRow(ctx, `INSERT INTO model_deployments(workspace_id,connection_id,name,model_id,capabilities,is_default) SELECT $1,$2,$3,$4,$5,$6 WHERE EXISTS(SELECT 1 FROM model_connections WHERE id=$2 AND workspace_id=$1) RETURNING id,connection_id,name,model_id,is_default`, workspaceID, connectionID, name, modelID, raw, isDefault).Scan(&d.ID, &d.ConnectionID, &d.Name, &d.ModelID, &d.IsDefault)
	return d, err
}
func (s *Store) ListDeployments(ctx context.Context, workspaceID uuid.UUID) ([]Deployment, error) {
	rows, err := s.db.Query(ctx, `SELECT id,connection_id,name,model_id,is_default,capabilities FROM model_deployments WHERE workspace_id=$1 ORDER BY is_default DESC,created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Deployment{}
	for rows.Next() {
		var d Deployment
		var raw []byte
		if err = rows.Scan(&d.ID, &d.ConnectionID, &d.Name, &d.ModelID, &d.IsDefault, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &d.Capabilities)
		items = append(items, d)
	}
	return items, rows.Err()
}
func (s *Store) Client(ctx context.Context, workspaceID, deploymentID uuid.UUID) (ModelClient, Deployment, error) {
	var c Connection
	var d Deployment
	var raw []byte
	err := s.db.QueryRow(ctx, `SELECT d.id,d.connection_id,d.name,d.model_id,d.is_default,c.id,c.workspace_id,c.name,c.provider,c.protocol,COALESCE(c.endpoint,''),c.config,c.credential_id FROM model_deployments d JOIN model_connections c ON c.id=d.connection_id WHERE d.id=$2 AND d.workspace_id=$1`, workspaceID, deploymentID).Scan(&d.ID, &d.ConnectionID, &d.Name, &d.ModelID, &d.IsDefault, &c.ID, &c.WorkspaceID, &c.Name, &c.Provider, &c.Protocol, &c.Endpoint, &raw, &c.CredentialID)
	if err != nil {
		return nil, d, err
	}
	_ = json.Unmarshal(raw, &c.Config)
	credential, err := s.secrets.Get(ctx, workspaceID, c.CredentialID)
	if err != nil {
		return nil, d, err
	}
	if c.Provider == "bedrock" {
		client, clientErr := NewBedrockClient(c.Config, credential)
		return client, d, clientErr
	}
	headers := map[string]string{}
	if c.Provider == "azure_openai" {
		headers["api-key"] = string(credential)
		credential = []byte("")
	}
	mode := ""
	if c.Provider == "vertex" {
		region, project := asString(c.Config["region"]), asString(c.Config["project"])
		if region == "" || project == "" {
			return nil, d, errors.New("Vertex requires project and region")
		}
		c.Endpoint = fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic/models/%s:streamRawPredict", region, project, region, d.ModelID)
		headers["Authorization"] = "Bearer " + string(credential)
		credential = []byte("")
		mode = "vertex"
	}
	if c.Provider == "foundry" {
		headers["api-key"] = string(credential)
		credential = []byte("")
	}
	return &HTTPClient{Protocol: c.Protocol, Endpoint: c.Endpoint, APIKey: string(credential), Headers: headers, Mode: mode}, d, nil
}
func defaultEndpoint(provider string, config map[string]any) string {
	switch provider {
	case "openai":
		return "https://api.openai.com/v1/chat/completions"
	case "anthropic":
		return "https://api.anthropic.com/v1/messages"
	case "azure_openai":
		return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s", strings.TrimRight(asString(config["resource_endpoint"]), "/"), asString(config["deployment"]), asString(config["api_version"]))
	case "foundry":
		return strings.TrimRight(asString(config["resource_endpoint"]), "/") + "/anthropic/v1/messages"
	}
	return asString(config["endpoint"])
}
