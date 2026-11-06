package hosted

import "net/http"

// Authorize confirms that the caller's verified provider identity also matches
// this deployment's server-only identity policy.
func Authorize(w http.ResponseWriter, r *http.Request) {
	AuthorizeWithConfig(BearerAuthConfigFromEnvironment()).ServeHTTP(w, r)
}

// AuthorizeWithConfig is the injectable form used by tests.
func AuthorizeWithConfig(config BearerAuthConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		RequireUserWithConfig(config, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(w, r)
	})
}
