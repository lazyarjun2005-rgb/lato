// Execution limits (Phase 2B). The budget, consecutive-failure cap,
// and provider retry count survive a bare test runtime that has no
// *config.Config: they fall back to the package defaults so the loop is
// always bounded, even outside a full configuration.
package runtime

import (
	"lato/internal/config"
)

// limits resolves the runtime's effective limits. With a nil config
// (bare test runtime) it returns a full set of safe defaults.
func (r *Runtime) limits() config.Limits {
	if r.cfg == nil {
		return config.Limits{
			MaxToolCalls:           100,
			MaxConsecutiveFailures: 5,
			ProviderRetries:        3,
		}
	}
	return r.cfg.EffectiveLimits()
}
