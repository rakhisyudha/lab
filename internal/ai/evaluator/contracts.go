package evaluator

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakhisyudha/lab/internal/ai/prompt"
)

// LoadContracts membaca Prompt_Contract aktif dari database, dipetakan per task_type.
func LoadContracts(ctx context.Context, pool *pgxpool.Pool) (map[string]prompt.Contract, error) {
	rows, err := pool.Query(ctx, `SELECT task_type, version, contract_id, locale_policy,
		system_prompt, user_prompt_template, input_schema, output_schema, parameters, status
		FROM prompt_contracts WHERE status = 'active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]prompt.Contract{}
	for rows.Next() {
		var c prompt.Contract
		var input, output, params []byte
		if err := rows.Scan(&c.TaskType, &c.Version, &c.ContractID, &c.LocalePolicy,
			&c.SystemPrompt, &c.UserPromptTemplate, &input, &output, &params, &c.Status); err != nil {
			return nil, err
		}
		c.InputSchema = json.RawMessage(input)
		c.OutputSchema = json.RawMessage(output)
		c.Parameters = json.RawMessage(params)
		out[c.TaskType] = c
	}
	return out, rows.Err()
}
