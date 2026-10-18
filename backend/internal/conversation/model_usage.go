package conversation

import (
	"context"
	"encoding/json"
	"time"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
	"github.com/google/uuid"
)

func (s *Service) startModelRequest(ctx context.Context, runID uuid.UUID, purpose string, budget toolcontext.Budget) (int, error) {
	raw, err := json.Marshal(budget)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM runs WHERE id=$1 FOR UPDATE`, runID); err != nil {
		return 0, err
	}
	var index int
	err = tx.QueryRow(ctx, `INSERT INTO model_request_usage(run_id,request_index,purpose,status,estimated_input_tokens,budget) SELECT $1,COALESCE(MAX(request_index),0)+1,$2,'started',$3,$4 FROM model_request_usage WHERE run_id=$1 RETURNING request_index`, runID, purpose, budget.EstimatedInput, raw).Scan(&index)
	if err != nil {
		return 0, err
	}
	return index, tx.Commit(ctx)
}
func (s *Service) finishModelRequest(ctx context.Context, runID uuid.UUID, index int, usage *modelruntime.NormalizedUsage, requestErr error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if usage == nil {
		usage = &modelruntime.NormalizedUsage{Source: modelruntime.UsageMissing}
	}
	raw, err := json.Marshal(usage)
	if err != nil {
		return err
	}
	status, kind := "completed", ""
	if requestErr != nil {
		status = "failed"
		kind = string(modelruntime.ErrorKindOf(requestErr))
		if kind == string(modelruntime.ErrorCancelled) {
			status = "cancelled"
		}
	}
	_, err = s.db.Exec(ctx, `UPDATE model_request_usage SET status=$3,usage=$4,error_kind=NULLIF($5,''),finished_at=now() WHERE run_id=$1 AND request_index=$2`, runID, index, status, raw, kind)
	return err
}
