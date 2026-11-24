package worker

import (
	"context"
	"errors"

	"github.com/rakhisyudha/lab/internal/ai/evalresult"
	"github.com/rakhisyudha/lab/internal/ai/evaluator"
	"github.com/rakhisyudha/lab/internal/ai/gateway"
	"github.com/rakhisyudha/lab/internal/ai/taskqueue"
)

// AnswerEvaluationHandler menjalankan Evaluator lalu menyimpan hasilnya.
func AnswerEvaluationHandler(ev *evaluator.Service, res *evalresult.Service) Handler {
	return func(ctx context.Context, task taskqueue.Task) error {
		result, err := ev.Evaluate(ctx, task)
		if err != nil {
			var throttle *gateway.ThrottleInfo
			if errors.As(err, &throttle) {
				return res.HandleFailure(ctx, task, "ai_provider_unavailable")
			}
			return res.HandleFailure(ctx, task, "validation_failed")
		}
		return res.HandleSuccess(ctx, task, result)
	}
}
