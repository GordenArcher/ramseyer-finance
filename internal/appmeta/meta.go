package appmeta

const (
	// CurrentVersion is the version embedded into the running binary. I keep it in one shared
	// package so the dashboard, setup page, updater checks, packaging scripts, and any future
	// diagnostics all speak the same version string instead of drifting into separate copies.
	CurrentVersion = "v1.2.3"

	// GitHubOwner and GitHubRepo define the release source that the in-app updater checks.
	// This is intentionally explicit instead of inferred from git remotes because production
	// binaries should know exactly which public release channel they trust.
	GitHubOwner = "GordenArcher"
	GitHubRepo  = "ramseyer-finance"

	// WindowsReleaseAssetName is the CLI-installable Windows archive uploaded by the release
	// workflow. Its stable name lets both the browser UI and terminal command identify a release.
	WindowsReleaseAssetName = "ramseyer-finance-windows-amd64.zip"
)
