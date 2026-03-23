package session

// SetupGCloud adds gcloud configuration env vars to the session.
// configName is the named gcloud configuration (e.g., "dev-project").
// Empty configName means use whatever the active configuration is.
func SetupGCloud(sess *Session, configName string) {
	if configName != "" {
		sess.Env = append(sess.Env, "CLOUDSDK_ACTIVE_CONFIG_NAME="+configName)
	}
	// CLOUDSDK_CONFIG points gcloud at the real config dir but active config
	// is overridden by CLOUDSDK_ACTIVE_CONFIG_NAME above.
	// We do NOT redirect CLOUDSDK_CONFIG to a fake dir because gcloud needs
	// to read the named configuration's credentials from the real config.
}
