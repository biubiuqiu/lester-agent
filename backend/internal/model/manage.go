package model

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrDeploymentNotFound = errors.New("model deployment not found")

type DeploymentBudgetChange struct {
	ContextWindow *int `json:"context_window,omitempty"`
	OutputReserve *int `json:"output_reserve,omitempty"`
	PolicyWindow  *int `json:"policy_window,omitempty"`
}

func (in DeploymentBudgetChange) hasChanges() bool {
	return in.ContextWindow != nil || in.OutputReserve != nil || in.PolicyWindow != nil
}

func (in DeploymentBudgetChange) apply(current Deployment) (Deployment, error) {
	for _, item := range []struct {
		value  *int
		target *int
	}{{in.ContextWindow, &current.ContextWindow}, {in.OutputReserve, &current.OutputReserve}, {in.PolicyWindow, &current.PolicyWindow}} {
		if item.value == nil {
			continue
		}
		if *item.value < 0 || int64(*item.value) > 2147483647 {
			return Deployment{}, errors.New("token budget values must be nonnegative integers within the supported range")
		}
		*item.target = *item.value
	}
	if current.ContextWindow > 0 && current.OutputReserve >= current.ContextWindow || current.PolicyWindow > 0 && current.OutputReserve >= current.PolicyWindow {
		return Deployment{}, errors.New("output reserve must be smaller than the configured context and policy windows")
	}
	return current, nil
}

// SetDeploymentBudget changes only supplied fields and never clears a default,
// disables a deployment, or crosses the deployment's owning workspace.
func (s *Store) SetDeploymentBudget(ctx context.Context, workspace, id uuid.UUID, in DeploymentBudgetChange) error {
	if !in.hasChanges() {
		return errors.New("at least one token budget value is required")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR UPDATE`, workspace); err != nil {
		return err
	}
	var current Deployment
	if err = tx.QueryRow(ctx, `SELECT context_window,output_reserve,policy_window FROM model_deployments WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspace, id).Scan(&current.ContextWindow, &current.OutputReserve, &current.PolicyWindow); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDeploymentNotFound
		}
		return err
	}
	updated, err := in.apply(current)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE model_deployments SET context_window=$3,output_reserve=$4,policy_window=$5 WHERE workspace_id=$1 AND id=$2`, workspace, id, updated.ContextWindow, updated.OutputReserve, updated.PolicyWindow); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type DeploymentChange struct {
	Name         string    `json:"name"`
	ModelID      string    `json:"model_id"`
	ConnectionID uuid.UUID `json:"connection_id"`
	IsDefault    bool      `json:"is_default"`
	Enabled      bool      `json:"enabled"`
	DeploymentBudgetChange
}

func (s *Store) UpdateDeployment(ctx context.Context, workspace, id uuid.UUID, in DeploymentChange) error {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.ModelID) == "" || len(in.Name) > 240 || len(in.ModelID) > 240 {
		return errors.New("name and model ID required")
	}
	if !in.Enabled {
		in.IsDefault = false
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR UPDATE`, workspace); err != nil {
		return err
	}
	var current Deployment
	if err = tx.QueryRow(ctx, `SELECT context_window,output_reserve,policy_window FROM model_deployments WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspace, id).Scan(&current.ContextWindow, &current.OutputReserve, &current.PolicyWindow); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDeploymentNotFound
		}
		return err
	}
	updated, err := in.DeploymentBudgetChange.apply(current)
	if err != nil {
		return err
	}
	if in.IsDefault {
		if _, err = tx.Exec(ctx, `UPDATE model_deployments SET is_default=false WHERE workspace_id=$1`, workspace); err != nil {
			return err
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE model_deployments SET name=$3,model_id=$4,is_default=$5,enabled=$6,connection_id=$7,context_window=$8,output_reserve=$9,policy_window=$10 WHERE workspace_id=$1 AND id=$2 AND EXISTS(SELECT 1 FROM model_connections WHERE id=$7 AND workspace_id=$1)`, workspace, id, strings.TrimSpace(in.Name), strings.TrimSpace(in.ModelID), in.IsDefault, in.Enabled, in.ConnectionID, updated.ContextWindow, updated.OutputReserve, updated.PolicyWindow)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("model or connection not found")
	}
	return tx.Commit(ctx)
}

type ConnectionChange struct {
	Name       string         `json:"name"`
	Endpoint   string         `json:"endpoint"`
	Config     map[string]any `json:"config"`
	Credential string         `json:"credential"`
}

func (s *Store) UpdateConnection(ctx context.Context, workspace, id uuid.UUID, in ConnectionChange) error {
	if strings.TrimSpace(in.Name) == "" || len([]rune(in.Name)) > 120 {
		return errors.New("name required")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var provider string
	if err = tx.QueryRow(ctx, `SELECT provider FROM model_connections WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspace, id).Scan(&provider); err != nil {
		return err
	}
	integrationProvider, err := s.providers.Resolve(provider)
	if err != nil {
		return err
	}
	if in.Endpoint == "" {
		in.Endpoint = integrationProvider.DefaultEndpoint(in.Config)
	}
	raw, err := json.Marshal(in.Config)
	if err != nil {
		return err
	}
	var credentialID *uuid.UUID
	if in.Credential != "" {
		newID, e := s.secrets.Put(ctx, workspace, []byte(in.Credential))
		if e != nil {
			return e
		}
		credentialID = &newID
	}
	if _, err = tx.Exec(ctx, `UPDATE model_connections SET name=$3,endpoint=$4,config=$5,credential_id=COALESCE($6,credential_id) WHERE workspace_id=$1 AND id=$2`, workspace, id, strings.TrimSpace(in.Name), in.Endpoint, raw, credentialID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
