package exporter

import "github.com/prometheus/client_golang/prometheus"

const (
	namespace = "kafka"
	clientID  = "kafka_exporter"
)

const (
	INFO  = 0
	DEBUG = 1
	TRACE = 2
)

var (
	clusterBrokers                     *prometheus.Desc
	clusterBrokerInfo                  *prometheus.Desc
	topicPartitions                    *prometheus.Desc
	topicCurrentOffset                 *prometheus.Desc
	topicOldestOffset                  *prometheus.Desc
	topicPartitionLeader               *prometheus.Desc
	topicPartitionReplicas             *prometheus.Desc
	topicPartitionInSyncReplicas       *prometheus.Desc
	topicPartitionUsesPreferredReplica *prometheus.Desc
	topicUnderReplicatedPartition      *prometheus.Desc
	consumergroupCurrentOffset         *prometheus.Desc
	consumergroupCurrentOffsetSum      *prometheus.Desc
	consumergroupLag                   *prometheus.Desc
	consumergroupLagSum                *prometheus.Desc
	consumergroupLagZookeeper          *prometheus.Desc
	consumergroupMembers               *prometheus.Desc
)

type KafkaOpts struct {
	Uri                      []string
	UseSASL                  bool
	UseSASLHandshake         bool
	SaslUsername             string
	SaslPassword             string
	SaslMechanism            string
	SaslDisablePAFXFast      bool
	UseTLS                   bool
	TlsServerName            string
	TlsCAFile                string
	TlsCertFile              string
	TlsKeyFile               string
	ServerUseTLS             bool
	ServerMutualAuthEnabled  bool
	ServerTlsCAFile          string
	ServerTlsCertFile        string
	ServerTlsKeyFile         string
	TlsInsecureSkipTLSVerify bool
	KafkaVersion             string
	UseZooKeeperLag          bool
	UriZookeeper             []string
	Labels                   string
	MetadataRefreshInterval  string
	ServiceName              string
	KerberosConfigPath       string
	Realm                    string
	KeyTabPath               string
	KerberosAuthType         string
	OffsetShowAll            bool
	TopicWorkers             int
	AllowConcurrent          bool
	AllowAutoTopicCreation   bool
	VerbosityLogLevel        int
}
