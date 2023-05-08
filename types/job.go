package types

type JobStatus string

// type Job interface {
// 	// job information
// 	ID() string
// 	Type() int
// 	// retry
// 	RetryCount() int
// 	NextTry() int64
// 	MaxTry() int
// 	// time
// 	At() int64
// 	EnqueueAt() int64
// 	CreatedAt() int64
// 	// data
// 	ErrorMessage() string
// 	Payload() map[string]interface{}
// }

type Job[T any] struct {
	ID    int    `json:"id,omitempty"`
	Type  int    `json:"type,omitempty"`
	Name  string `json:"name,omitempty"`
	Event string `json:"event,omitempty"`

	RetryCount int   `json:"retry_count,omitempty"`
	MaxTry     int   `json:"max_try,omitempty"`
	At         int64 `json:"at,omitempty"`
	EnqueueAt  int64 `json:"enqueue_at"`
	CreatedAt  int64 `json:"created_at"`

	Data      T                      `json:"data"`
	ExtraData map[string]interface{} `json:"extra_data"`

	Error error `json:"error,omitempty"`
}

// job information
// func (j *job) ID() string {
// 	panic("not implemented") // TODO: Implement
// }

// func (j *job) Type() int {
// 	panic("not implemented") // TODO: Implement
// }

// // retry
// func (j *job) RetryCount() int {
// 	panic("not implemented") // TODO: Implement
// }

// func (j *job) NextTry() int64 {
// 	panic("not implemented") // TODO: Implement
// }

// func (j *job) MaxTry() int {
// 	panic("not implemented") // TODO: Implement
// }

// // time
// func (j *job) At() int64 {
// 	panic("not implemented") // TODO: Implement
// }

// func (j *job) EnqueueAt() int64 {
// 	panic("not implemented") // TODO: Implement
// }

// func (j *job) CreatedAt() int64 {
// 	panic("not implemented") // TODO: Implement
// }

// // data
// func (j *job) ErrorMessage() string {
// 	panic("not implemented") // TODO: Implement
// }

// func (j *job) Payload() map[string]interface{} {
// 	panic("not implemented") // TODO: Implement
// }

// func (j *job) MarkAsError(err error) {
// 	if err == nil {
// 		return
// 	}

// 	j.err = err
// }
