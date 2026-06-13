package modelrole

import "github.com/choiceoh/deneb/gateway-go/internal/ai/router"

// RoutingProfileForModel resolves the effective effort-routing policy for a
// provider/model pair by layering, lowest to highest precedence:
//
//  1. router.DefaultProfile() — the shipped baseline heuristics.
//  2. The model's capability toggle (modelcaps.ThinkingToggleKwarg via
//     CapabilityForModel): a non-empty kwarg enables routing by default, so a
//     dual-mode model works out of the box and any other model stays inert.
//  3. deneb.json models.providers.<id>.routing overrides — operator tuning.
//
// The current main model has no routing override, so it resolves to
// DefaultProfile() + its capability toggle: identical to the pre-config
// behavior. A model with no toggle and no override resolves Enabled=false.
func (r *Registry) RoutingProfileForModel(providerID, model string) router.Profile {
	p := router.DefaultProfile()
	p.ToggleKwarg = r.CapabilityForModel(providerID, model).ThinkingToggleKwarg
	p.Enabled = p.ToggleKwarg != ""

	r.mu.RLock()
	pr, ok := r.providers[providerID]
	r.mu.RUnlock()
	if ok && pr.Routing != nil {
		applyRoutingOverride(&p, pr.Routing)
	}
	return p
}

// applyRoutingOverride layers a deneb.json routing block over the resolved
// profile. Only non-nil fields take effect; ToggleKwarg is applied before
// Enabled so an override can both name a toggle and turn routing on in one
// block, while an explicit Enabled still wins when present.
func applyRoutingOverride(p *router.Profile, o *RoutingOverride) {
	if o.ToggleKwarg != nil {
		p.ToggleKwarg = *o.ToggleKwarg
		p.Enabled = p.ToggleKwarg != ""
	}
	if o.Enabled != nil {
		p.Enabled = *o.Enabled
	}
	if o.MaxSimpleRunes != nil {
		p.MaxSimpleRunes = *o.MaxSimpleRunes
	}
	if o.StepCeilingTurn != nil {
		p.StepCeilingTurn = *o.StepCeilingTurn
	}
	if o.ObservationRunes != nil {
		p.ObservationRunes = *o.ObservationRunes
	}
	if o.CumulativeRunes != nil {
		p.CumulativeRunes = *o.CumulativeRunes
	}
	if o.HeavyHistoryRunes != nil {
		p.HeavyHistoryRunes = *o.HeavyHistoryRunes
	}
}
