package domain

import "time"

type CorrelationID string
type Adapter string
type OperationName string

type Operation struct {
	Name          OperationName
	Adapter       Adapter
	CorrelationID CorrelationID
	StartedAt     time.Time
	RetryCount    int
}

type Result struct {
	Changed bool
	Summary string
}
