package conversation

import (
	"context"
	"encoding/json"
	"time"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
	"github.com/google/uuid"
)

func (s *Service) startModelRequest(ctx context.Context, runID uuid.UUID, index int, purpose string, budget toolcontext.Budget) error {
	raw, err := json.Marshal(budget)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO model_request_usage(run_id,request_index,purpose,status,estimated_input_tokens,budget) VALUES($1,$2,$3,'started',$4,$5)`, runID, index, purpose, budget.EstimatedInput, raw)
	return err
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
