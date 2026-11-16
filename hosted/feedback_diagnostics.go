package hosted

import (
	"errors"
	"log"
)

// Diagnostic errors retain only a fixed phase and a safe sentinel, never the
// driver/configuration error, SQL, account, invitation, or secret value.
type feedbackDiagnosticPhase uint8

const (
	fbConfigProvenance feedbackDiagnosticPhase = iota
	fbConfigPublic
	fbConfigAuthProvider
	fbConfigKeyRing
	fbStoreDSN
	fbStoreInput
	fbStoreBegin
	fbStoreTimeouts
	fbStoreRandom
	fbStoreInsert
	fbStoreSelect
	fbStoreUpdate
	fbStoreCommit
)

type feedbackDiagnostic struct {
	phase    feedbackDiagnosticPhase
	sentinel error
}

func (e feedbackDiagnostic) Error() string { return "UNAVAILABLE" }
func (e feedbackDiagnostic) Unwrap() error { return e.sentinel }
func logFeedbackDiagnostic(err error) {
	var d feedbackDiagnostic
	if !errors.As(err, &d) {
		return
	}
	// No formatting of the error or underlying values. Unknown phases stay safe.
	codes := [...]string{"FB_CONFIG_PROVENANCE", "FB_CONFIG_PUBLIC", "FB_CONFIG_AUTH_PROVIDER", "FB_CONFIG_KEY_RING", "FB_STORE_DSN", "FB_STORE_INPUT", "FB_STORE_BEGIN", "FB_STORE_TIMEOUTS", "FB_STORE_RANDOM", "FB_STORE_INSERT", "FB_STORE_SELECT", "FB_STORE_UPDATE", "FB_STORE_COMMIT"}
	if int(d.phase) >= len(codes) {
		log.Print("FB_DIAGNOSTIC_UNKNOWN")
		return
	}
	log.Print(codes[d.phase])
}
