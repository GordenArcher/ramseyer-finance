package appmeta

const (
	// CurrentVersion is the version embedded into the running binary. I keep it in one shared
	// package so the dashboard, setup page, updater checks, packaging scripts, and any future
	// diagnostics all speak the same version string instead of drifting into separate copies.
	CurrentVersion = "v1.2.0"

	// GitHubOwner and GitHubRepo define the release source that the in-app updater checks.
	// This is intentionally explicit instead of inferred from git remotes because production
	// binaries should know exactly which public release channel they trust.
	GitHubOwner = "GordenArcher"
	GitHubRepo  = "ramseyer-finance"

	// WindowsReleaseAssetName is the packaged bundle uploaded to GitHub Releases and consumed
	// by the updater. The updater searches for this asset in the latest release.
	WindowsReleaseAssetName = "Ramseyer Finance-windows.zip"
)
