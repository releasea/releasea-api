package models

type ControlPlaneMetrics struct {
	Version string                   `json:"version"`
	Queue   ControlPlaneQueueMetrics `json:"queue"`
}

type ControlPlaneQueueMetrics struct {
	QueueName                string `json:"queueName"`
	DeadLetterEnabled        bool   `json:"deadLetterEnabled"`
	DeadLetterQueueName      string `json:"deadLetterQueueName,omitempty"`
	QueuedOperations         int64  `json:"queuedOperations"`
	DispatchingOperations    int64  `json:"dispatchingOperations"`
	DispatchFailedOperations int64  `json:"dispatchFailedOperations"`
	StaleQueuedOperations    int64  `json:"staleQueuedOperations"`
	RecentDispatchFailures   int64  `json:"recentDispatchFailures"`
	OldestQueuedAt           string `json:"oldestQueuedAt,omitempty"`
	OldestQueuedAgeSeconds   int64  `json:"oldestQueuedAgeSeconds,omitempty"`
	LastDispatchFailureAt    string `json:"lastDispatchFailureAt,omitempty"`
	Status                   string `json:"status"`
	Summary                  string `json:"summary"`
}
