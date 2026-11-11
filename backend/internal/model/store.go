package model

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/biubiuqiu/lester-agent/backend/internal/model/integration"
	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
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
type DesktopProvider interface {
	DesktopClient(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) (ModelClient, error)
	DesktopDeviceOnline(uuid.UUID) bool
}

func (s *Store) SetDesktopProvider(provider DesktopProvider) { s.desktop = provider }

type Deployment struct {
	Source        string            `json:"source"`
	DeviceID      uuid.UUID         `json:"device_id,omitempty"`
	DeviceName    string            `json:"device_name,omitempty"`
	Online        bool              `json:"online"`
	ReadOnly      bool              `json:"read_only"`
	ID            uuid.UUID         `json:"id"`
	ConnectionID  uuid.UUID         `json:"connection_id"`
	Name          string            `json:"name"`
	ModelID       string            `json:"model_id"`
	IsDefault     bool              `json:"is_default"`
	Enabled       bool              `json:"enabled"`
	Shared        bool              `json:"shared"`
	Capabilities  ModelCapabilities `json:"capabilities"`
	ContextWindow int               `json:"context_window"`
	OutputReserve int               `json:"output_reserve"`
	PolicyWindow  int               `json:"policy_window"`
}
type Store struct {
	db        *pgxpool.Pool
	secrets   *secret.Store
	providers *integration.Registry
	desktop   DesktopProvider
}

// SystemWorkspaceID is reserved by migration 000007 and has no user members.
var SystemWorkspaceID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

func NewStore(db *pgxpool.Pool, secrets *secret.Store, providers *integration.Registry) *Store {
	return &Store{db: db, secrets: secrets, providers: providers}
}
func (s *Store) CreateConnection(ctx context.Context, workspaceID uuid.UUID, name, provider, endpoint string, config map[string]any, credential string) (Connection, error) {
	if strings.TrimSpace(name) == "" || len([]rune(name)) > 120 || credential == "" {
		return Connection{}, errors.New("connection name and credential are required")
	}
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
	rows, err := s.db.Query(ctx, `SELECT id,workspace_id,name,provider,protocol,COALESCE(endpoint,''),config,credential_id FROM model_connections WHERE workspace_id=$1 AND provider<>'desktop' ORDER BY created_at`, workspaceID)
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
	return s.CreateDeploymentWithBudget(ctx, workspaceID, connectionID, name, modelID, isDefault, DeploymentBudgetChange{})
}

func (s *Store) CreateDeploymentWithBudget(ctx context.Context, workspaceID, connectionID uuid.UUID, name, modelID string, isDefault bool, budget DeploymentBudgetChange) (Deployment, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(modelID) == "" || len(name) > 240 || len(modelID) > 240 {
		return Deployment{}, errors.New("valid name and model ID are required")
	}
	limits, err := budget.apply(Deployment{})
	if err != nil {
		return Deployment{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Deployment{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR UPDATE`, workspaceID); err != nil {
		return Deployment{}, err
	}
	if isDefault {
		if _, err = tx.Exec(ctx, `UPDATE model_deployments SET is_default=false WHERE workspace_id=$1`, workspaceID); err != nil {
			return Deployment{}, err
		}
	}
	caps := ModelCapabilities{Streaming: true, Tools: true, Vision: true}
	raw, _ := json.Marshal(caps)
	var d Deployment
	d.Capabilities = caps
	err = tx.QueryRow(ctx, `INSERT INTO model_deployments(workspace_id,connection_id,name,model_id,capabilities,is_default,context_window,output_reserve,policy_window) SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9 WHERE EXISTS(SELECT 1 FROM model_connections WHERE id=$2 AND workspace_id=$1 AND provider<>'desktop') RETURNING id,connection_id,name,model_id,is_default,context_window,output_reserve,policy_window`, workspaceID, connectionID, name, modelID, raw, isDefault, limits.ContextWindow, limits.OutputReserve, limits.PolicyWindow).Scan(&d.ID, &d.ConnectionID, &d.Name, &d.ModelID, &d.IsDefault, &d.ContextWindow, &d.OutputReserve, &d.PolicyWindow)
	if err != nil {
		return d, err
	}
	d.Enabled = true
	d.Shared = workspaceID == SystemWorkspaceID
	return d, tx.Commit(ctx)
}
func (s *Store) ListDeployments(ctx context.Context, workspaceID uuid.UUID) ([]Deployment, error) {
	actor, _ := modelruntime.Actor(ctx)
	rows, err := s.db.Query(ctx, `SELECT d.id,d.connection_id,d.name,d.model_id,d.is_default,d.capabilities,d.enabled,d.workspace_id=$2,d.context_window,d.output_reserve,d.policy_window,dm.device_id,COALESCE(dd.name,'') FROM model_deployments d JOIN model_connections c ON c.id=d.connection_id AND c.workspace_id=d.workspace_id LEFT JOIN desktop_device_models dm ON dm.deployment_id=d.id LEFT JOIN desktop_devices dd ON dd.id=dm.device_id WHERE (d.workspace_id=$1 OR d.workspace_id=$2) AND (d.enabled OR $1=$2) AND (c.provider<>'desktop' OR (dd.user_id=$3 AND dd.workspace_id=$1 AND dd.revoked_at IS NULL AND dd.token_expires_at>now() AND EXISTS(SELECT 1 FROM sessions s WHERE s.id=dd.session_id AND s.expires_at>now()))) ORDER BY d.is_default DESC,(d.workspace_id=$1) DESC,d.created_at`, workspaceID, SystemWorkspaceID, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Deployment{}
	for rows.Next() {
		var d Deployment
		var raw []byte
		var device *uuid.UUID
		if err = rows.Scan(&d.ID, &d.ConnectionID, &d.Name, &d.ModelID, &d.IsDefault, &raw, &d.Enabled, &d.Shared, &d.ContextWindow, &d.OutputReserve, &d.PolicyWindow, &device, &d.DeviceName); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &d.Capabilities)
		d.Capabilities.TokenCounting = false
		d.Capabilities.StructuredOutput = false
		d.Source = "cloud"
		d.Online = true
		if device != nil {
			d.DeviceID = *device
			d.Source = "device"
			d.ReadOnly = true
			d.Online = s.desktop != nil && s.desktop.DesktopDeviceOnline(*device)
		}
		items = append(items, d)
	}
	return items, rows.Err()
}
func (s *Store) Client(ctx context.Context, workspaceID, deploymentID uuid.UUID) (ModelClient, Deployment, error) {
	var c Connection
	var d Deployment
	var raw []byte
	var capabilities []byte
	var credentialID *uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT d.id,d.connection_id,d.name,d.model_id,d.is_default,d.enabled,d.workspace_id=$3,d.context_window,d.output_reserve,d.policy_window,d.capabilities,c.id,c.workspace_id,c.name,c.provider,c.protocol,COALESCE(c.endpoint,''),c.config,c.credential_id FROM model_deployments d JOIN model_connections c ON c.id=d.connection_id AND c.workspace_id=d.workspace_id WHERE d.id=$2 AND (d.workspace_id=$1 OR d.workspace_id=$3) AND d.enabled`, workspaceID, deploymentID, SystemWorkspaceID).Scan(&d.ID, &d.ConnectionID, &d.Name, &d.ModelID, &d.IsDefault, &d.Enabled, &d.Shared, &d.ContextWindow, &d.OutputReserve, &d.PolicyWindow, &capabilities, &c.ID, &c.WorkspaceID, &c.Name, &c.Provider, &c.Protocol, &c.Endpoint, &raw, &credentialID)
	if err != nil {
		return nil, d, err
	}
	_ = json.Unmarshal(raw, &c.Config)
	_ = json.Unmarshal(capabilities, &d.Capabilities)
	d.Capabilities.TokenCounting = false
	d.Capabilities.StructuredOutput = false
	d.Source = "cloud"
	d.Online = true
	if c.Provider == "desktop" {
		actor, ok := modelruntime.Actor(ctx)
		if !ok || s.desktop == nil {
			return nil, d, ErrDeploymentNotFound
		}
		var device, owner uuid.UUID
		var key, name string
		err = s.db.QueryRow(ctx, `SELECT dd.id,dd.user_id,dm.model_key,dd.name FROM desktop_device_models dm JOIN desktop_devices dd ON dd.id=dm.device_id JOIN sessions ss ON ss.id=dd.session_id WHERE dm.deployment_id=$1 AND dd.workspace_id=$2 AND dd.user_id=$3 AND dd.revoked_at IS NULL AND dd.token_expires_at>now() AND ss.expires_at>now()`, deploymentID, workspaceID, actor).Scan(&device, &owner, &key, &name)
		if err != nil {
			return nil, d, ErrDeploymentNotFound
		}
		d.Source = "device"
		d.DeviceID = device
		d.DeviceName = name
		d.Online = s.desktop.DesktopDeviceOnline(device)
		d.ReadOnly = true
		client, err := s.desktop.DesktopClient(ctx, workspaceID, owner, device, key)
		return client, d, err
	}
	if credentialID == nil {
		return nil, d, errors.New("model credential unavailable")
	}
	c.CredentialID = *credentialID
	credential, err := s.secrets.Get(ctx, c.WorkspaceID, c.CredentialID)
	if err != nil {
		return nil, d, err
	}
	client, err := s.providers.NewClient(integration.ClientSpec{Provider: c.Provider, Protocol: c.Protocol, Endpoint: c.Endpoint, ModelID: d.ModelID, Config: c.Config, Credential: credential})
	return client, d, err
}
