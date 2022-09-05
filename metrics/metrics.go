package metrics

import (
	"context"

	"github.com/axieinfinity/bridge-core/adapters/prometheus"
)

const (
	ListenerProcessedBlockMetric string = "%s_processedBlock"

	PreparingFailedJobMetric  string = "jobs_prepare_failed"
	PreparingSuccessJobMetric string = "jobs_prepare_success"
	ProcessingJobMetric       string = "jobs_processing"
	ProcessedSuccessJobMetric string = "jobs_success"
	ProcessedFailedJobMetric  string = "jobs_failed"

	PendingTaskMetric       string = "Ronin_tasks_pending"
	ProcessingTaskMetric    string = "Ronin_tasks_processing"
	SuccessTaskMetric       string = "Ronin_tasks_success"
	FailedTaskMetric        string = "Ronin_tasks_failed"
	WithdrawalTaskMetric    string = "Ronin_tasks_withdrawal"
	DepositTaskMetric       string = "Ronin_tasks_deposit"
	AckWithdrawalTaskMetric string = "Ronin_tasks_acknowledgeWithdrawal"

	KmsSuccessSign     string = "kms_success"
	KmsNetworkFailure  string = "kms_failure_network"
	KmsInternalFailure string = "kms_failure_internal"
	KmsSignLatency     string = "kms_latency"
	KmsLastSuccess     string = "kms_lastSuccess"
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
	Pusher.AddGauge(ProcessingTaskMetric, "count number of processing tasks")
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
