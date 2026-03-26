// queue_cleanup.go — Session queue cleanup.
// Mirrors src/auto-reply/reply/queue/cleanup.ts (34 LOC).
package queue

// ClearSessionQueueResult holds the result of clearing session queues.
type ClearSessionQueueResult struct {
	FollowupCleared int      `json:"followupCleared"`
	Keys            []string `json:"keys"`
}

// ClearSessionQueues clears followup queues for the given session keys.
func ClearSessionQueues(
	registry *FollowupQueueRegistry,
	drainCallbacks *FollowupDrainCallbacks,
	keys []string,
) ClearSessionQueueResult {
	seen := make(map[string]bool)
	followupCleared := 0
	var clearedKeys []string

	for _, key := range keys {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		clearedKeys = append(clearedKeys, key)
		followupCleared += registry.Clear(key)
		if drainCallbacks != nil {
			drainCallbacks.Delete(key)
		}
	}

	return ClearSessionQueueResult{
		FollowupCleared: followupCleared,
		Keys:            clearedKeys,
	}
}
