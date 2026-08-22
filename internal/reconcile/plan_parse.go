package reconcile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const MaxPlanBytes = 8 << 20

func ParsePlan(data []byte) (domain.Plan, error) {
	if len(data) > MaxPlanBytes {
		return domain.Plan{}, fmt.Errorf("plan exceeds size limit")
	}
	if err := inspectJSON(data); err != nil {
		return domain.Plan{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var plan domain.Plan
	if err := decoder.Decode(&plan); err != nil {
		return domain.Plan{}, fmt.Errorf("decode plan: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return domain.Plan{}, fmt.Errorf("plan contains trailing data")
	}
	if err := ValidatePlan(plan); err != nil {
		return domain.Plan{}, err
	}
	return plan, nil
}
