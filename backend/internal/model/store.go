package model

import (
	"context"
	"encoding/json"

	"github.com/biubiuqiu/lester-agent/backend/internal/model/integration"
	"github.com/biubiuqiu/lester-agent/backend/internal/secret"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
	db        *pgxpool.Pool
	secrets   *secret.Store
	providers *integration.Registry
}

func NewStore(db *pgxpool.Pool, secrets *secret.Store, providers *integration.Registry) *Store {
	return &Store{db: db, secrets: secrets, providers: providers}
}
func (s *Store) CreateConnection(ctx context.Context, workspaceID uuid.UUID, name, provider, endpoint string, config map[string]any, credential string) (Connection, error) {
	integrationProvider, err := s.providers.Resolve(provider)
	if err != nil {
		return Connection{}, err
	}
	if endpoint == "" {
		endpoint = integrationProvider.DefaultEndpoint(config)
	}
	credentialID, err := s.secrets.Put(ctx, workspaceID, []byte(credential))
	if err != nil {
		return Connection{}, err
	}
	raw, _ := json.Marshal(config)
	var result Connection
	result.Config = config
	result.CredentialID = credentialID
	err = s.db.QueryRow(ctx, `INSERT INTO model_connections(workspace_id,name,provider,protocol,endpoint,config,credential_id) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id,workspace_id,name,provider,protocol,endpoint`, workspaceID, name, provider, integrationProvider.Protocol(), endpoint, raw, credentialID).Scan(&result.ID, &result.WorkspaceID, &result.Name, &result.Provider, &result.Protocol, &result.Endpoint)
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
	client, err := s.providers.NewClient(integration.ClientSpec{Provider: c.Provider, Protocol: c.Protocol, Endpoint: c.Endpoint, ModelID: d.ModelID, Config: c.Config, Credential: credential})
	return client, d, err
}
