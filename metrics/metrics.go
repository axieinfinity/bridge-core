package metrics

import (
	"context"

	"github.com/axieinfinity/bridge-core/adapters/prometheus"
)

const (
	ListenerProcessedBlockMetric string = "%s/processedBlock"

	PreparingFailedJobMetric  string = "jobs/prepare/failed"
	PreparingSuccessJobMetric string = "jobs/prepare/success"
	ProcessingJobMetric       string = "jobs/processing"
	ProcessedSuccessJobMetric string = "jobs/success"
	ProcessedFailedJobMetric  string = "jobs/failed"

	PendingTaskMetric       string = "Ronin/tasks/pending"
	ProcessingTaskMetric    string = "Ronin/tasks/processing"
	SuccessTaskMetric       string = "Ronin/tasks/success"
	FailedTaskMetric        string = "Ronin/tasks/failed"
	WithdrawalTaskMetric    string = "Ronin/tasks/withdrawal"
	DepositTaskMetric       string = "Ronin/tasks/deposit"
	AckWithdrawalTaskMetric string = "Ronin/tasks/acknowledgeWithdrawal"

	KmsSuccessSign     string = "kms/success"
	KmsNetworkFailure  string = "kms/failure/network"
	KmsInternalFailure string = "kms/failure/internal"
	KmsSignLatency     string = "kms/latency"
	KmsLastSuccess     string = "kms/lastSuccess"
)

var (
	Pusher *prometheus.Pusher
)

func RunPusher(ctx context.Context) {
	Pusher = prometheus.NewPusher()
	Pusher.AddCounter(PreparingFailedJobMetric, "count number of preparing jobs failed to added to database")
	Pusher.AddCounter(PreparingSuccessJobMetric, "count number of preparing jobs added to database successfully and switch to new ")
	Pusher.AddGauge(ProcessingJobMetric, "count number of processing jobs in jobChan")
	Pusher.AddCounter(ProcessedSuccessJobMetric, "count number of processed jobs successfully")
	Pusher.AddCounter(ProcessedFailedJobMetric, "count number of failed jobs")
	Pusher.AddCounter(PendingTaskMetric, "count number of pending tasks in queue")
	Pusher.AddCounter(ProcessingTaskMetric, "count number of processing tasks")
	Pusher.AddCounter(ProcessedSuccessJobMetric, "count number of success tasks")
	Pusher.AddCounter(ProcessedFailedJobMetric, "count number failed tasks")
	Pusher.AddCounter(WithdrawalTaskMetric, "count number of ronin’s withdrawal tasks occurred")
	Pusher.AddCounter(DepositTaskMetric, "count number of ronin’s deposit tasks occurred")
	Pusher.AddCounter(AckWithdrawalTaskMetric, "count number of ronin acknowledge withdrawal tasks occurred")
	Pusher.AddCounter(KmsSuccessSign, "count number of successful KMS signs")
	Pusher.AddCounter(KmsNetworkFailure, "count number of failed KMS signs due to network")
	Pusher.AddCounter(KmsInternalFailure, "count number of KMS server error responses")
	Pusher.AddHistogram(KmsSignLatency, "the latency of signing request to KMS server in milliseconds")
	Pusher.AddGauge(KmsLastSuccess, "timestamp of last KMS successful signs")

	go Pusher.Start(ctx)
}
