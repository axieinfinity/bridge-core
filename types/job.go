package types

type JobStatus string

type Job interface {
	// job information
	ID() string
	Type() int
	// retry
	RetryCount() int
	NextTry() int64
	MaxTry() int
	// time
	At() int64
	EnqueueAt() int64
	CreatedAt() int64
	// data
	ErrorMessage() string
	Payload() map[string]interface{}

	MarkAsRunning()
	MarkAsRetrying()
	MarkAsError(error)
}

type job struct {
	id      string
	jobType int

	retryCount int32
	nextTry    int32
	maxTry     int32
	backOff    int32

	payload map[string]interface{}

	err error
}

// job information
func (j *job) ID() string {
	panic("not implemented") // TODO: Implement
}

func (j *job) Type() int {
	panic("not implemented") // TODO: Implement
}

// retry
func (j *job) RetryCount() int {
	panic("not implemented") // TODO: Implement
}

func (j *job) NextTry() int64 {
	panic("not implemented") // TODO: Implement
}

func (j *job) MaxTry() int {
	panic("not implemented") // TODO: Implement
}

// time
func (j *job) At() int64 {
	panic("not implemented") // TODO: Implement
}

func (j *job) EnqueueAt() int64 {
	panic("not implemented") // TODO: Implement
}

func (j *job) CreatedAt() int64 {
	panic("not implemented") // TODO: Implement
}

// data
func (j *job) ErrorMessage() string {
	panic("not implemented") // TODO: Implement
}

func (j *job) Payload() map[string]interface{} {
	panic("not implemented") // TODO: Implement
}

func (j *job) MarkAsError(err error) {
	if err == nil {
		return
	}

	j.err = err
}
