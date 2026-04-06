// Hand-written constants. Previously generated from YAML.

package memory

// categoryImportanceMultiplier adjusts the importance weight by fact category.
// Decisions, context, and solutions are factual records of what happened -> boost.
// User model and mutual are relational/personality data -> keep but don't over-boost.
var categoryImportanceMultiplier = map[string]float64{
	"decision":   1.2,
	"preference": 1.05,
	"solution":   1.1,
	"context":    0.95,
	"user_model": 1.0,
	"mutual":     0.85,
}

// categorySteepnessDays controls the inverse-square recency decay per category.
// The steepness value is the number of days at which score drops to 0.5.
// Curve: score = 1 / (1 + (days/steepness)^2)
// Lower values = faster decay.
var categorySteepnessDays = map[string]float64{
	"decision":   14.0,
	"preference": 10.0,
	"solution":   10.0,
	"context":    5.0,
	"user_model": 14.0,
	"mutual":     7.0,
}

// CategoryVolatileDays defines "shelf life" per fact category.
// Facts older than this threshold (relative to UpdatedAt) get a staleness hint.
// Aligned with categorySteepnessDays: shelfLife ~ steepness x 3.
var CategoryVolatileDays = map[string]int{
	"context":    45,
	"decision":   135,
	"solution":   90,
	"preference": 90,
	"user_model": 135,
	"mutual":     68,
}
