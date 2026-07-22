package contract

import (
	"fmt"
	"sync"

	"github.com/k911mipt/agent-managed-bash/internal/state"
	"github.com/k911mipt/agent-managed-bash/schemas"
)

type Contracts struct {
	policy         state.Policy
	stateValidator state.PersistedStateValidator
}

var loadOnce = sync.OnceValues(load)

func Load() (Contracts, error) {
	return loadOnce()
}

func (contracts Contracts) Policy() state.Policy {
	return contracts.policy
}

func (contracts Contracts) StateValidator() state.PersistedStateValidator {
	return contracts.stateValidator
}

func load() (Contracts, error) {
	assets := schemas.V1()
	policy, err := state.LoadPolicy(assets.PolicySchema(), assets.Policy())
	if err != nil {
		return Contracts{}, fmt.Errorf("load embedded policy: %w", err)
	}
	validator, err := state.NewPersistedStateValidator(assets.ModelsSchema(), assets.StateSchema(), policy)
	if err != nil {
		return Contracts{}, fmt.Errorf("load embedded state validator: %w", err)
	}
	return Contracts{policy: policy, stateValidator: validator}, nil
}
