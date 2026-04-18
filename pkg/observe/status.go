package observe

type StatusSnapshot struct {
	RunningRuns       int      `json:"runningRuns"`
	ReleaseWaitNodes  int      `json:"releaseWaitNodes"`
	BackendReady      bool     `json:"backendReady"`
	RecentErrors      []string `json:"recentErrors,omitempty"`
}
