package exporter

import "github.com/prometheus/client_golang/prometheus"

// Describe describes all the metrics ever exported by the Kafka exporter. It
// implements prometheus.Collector.
// 告诉 Prometheus，这个 exporter 会提供哪些指标，这些指标的基本信息是什么。
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- clusterBrokers
	ch <- topicCurrentOffset
	ch <- topicOldestOffset
	ch <- topicPartitions
	ch <- topicPartitionLeader
	ch <- topicPartitionReplicas
	ch <- topicPartitionInSyncReplicas
	ch <- topicPartitionUsesPreferredReplica
	ch <- topicUnderReplicatedPartition
	ch <- consumergroupCurrentOffset
	ch <- consumergroupCurrentOffsetSum
	ch <- consumergroupLag
	ch <- consumergroupLagZookeeper
	ch <- consumergroupLagSum
}
