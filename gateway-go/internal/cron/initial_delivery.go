// initial_delivery.go — Resolves default delivery for new cron jobs.
// Mirrors src/cron/service/initial-delivery.ts (35 LOC).
package cron

// ResolveInitialCronDelivery determines the initial delivery config for a new job.
// Auto-sets delivery to "announce" for isolated agentTurn jobs without explicit delivery.
func ResolveInitialCronDelivery(job StoreJob) *JobDeliveryConfig {
	if job.Delivery != nil {
		return job.Delivery
	}
	// Auto-enable delivery for isolated agent turn jobs.
	if job.Payload.Kind == "agentTurn" {
		return &JobDeliveryConfig{
			Channel: "last",
		}
	}
	return nil
}

// NormalizeCronCreateDeliveryInput applies legacy delivery hint merging on job creation.
// This is a simplified version for the structured Go types (vs the raw-map TS version).
func NormalizeCronCreateDeliveryInput(job *StoreJob) {
	if job == nil {
		return
	}
	// In Go, legacy fields are already handled during store migration.
	// This function ensures isolated agentTurn jobs get default delivery.
	if job.Delivery == nil && job.Payload.Kind == "agentTurn" {
		job.Delivery = &JobDeliveryConfig{
			Channel: "last",
		}
	}
}
