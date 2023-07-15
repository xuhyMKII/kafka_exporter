package main

import (
	"github.com/Shopify/sarama"
	kingpin "github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	plog "github.com/prometheus/common/promlog"
	plogflag "github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"github.com/rcrowley/go-metrics"
	"kafka_exporter/exporter"
	"kafka_exporter/tool"
	"strings"
)

func init() {
	metrics.UseNilMetrics = true
	//向 Prometheus 注册了一个新的收集器（Collector）。这个收集器用于收集名为 "kafka_exporter" 的版本信息。
	prometheus.MustRegister(version.NewCollector("kafka_exporter"))
}

func main() {
	var (
		listenAddress = tool.ToFlagString("web.listen-address", "Address to listen on for web interface and telemetry.", ":9308")
		metricsPath   = tool.ToFlagString("web.telemetry-path", "Path under which to expose metrics.", "/metrics")
		topicFilter   = tool.ToFlagString("topic.filter", "Regex that determines which topics to collect.", ".*")
		topicExclude  = tool.ToFlagString("topic.exclude", "Regex that determines which topics to exclude.", "^$")
		groupFilter   = tool.ToFlagString("group.filter", "Regex that determines which consumer groups to collect.", ".*")
		groupExclude  = tool.ToFlagString("group.exclude", "Regex that determines which consumer groups to exclude.", "^$")
		logSarama     = tool.ToFlagBool("log.enable-sarama", "Turn on Sarama logging, default is false.", false, "false")

		opts = exporter.KafkaOpts{}
	)

	tool.ToFlagStringsVar("kafka.server", "Address (host:port) of Kafka server.", "kafka:9092", &opts.Uri)
	tool.ToFlagBoolVar("sasl.enabled", "Connect using SASL/PLAIN, default is false.", false, "false", &opts.UseSASL)
	tool.ToFlagBoolVar("sasl.handshake", "Only set this to false if using a non-Kafka SASL proxy, default is true.", true, "true", &opts.UseSASLHandshake)
	tool.ToFlagStringVar("sasl.username", "SASL user name.", "", &opts.SaslUsername)
	tool.ToFlagStringVar("sasl.password", "SASL user password.", "", &opts.SaslPassword)
	tool.ToFlagStringVar("sasl.mechanism", "The SASL SCRAM SHA algorithm sha256 or sha512 or gssapi as mechanism", "", &opts.SaslMechanism)
	tool.ToFlagStringVar("sasl.service-name", "Service name when using kerberos Auth", "", &opts.ServiceName)
	tool.ToFlagStringVar("sasl.kerberos-config-path", "Kerberos config path", "", &opts.KerberosConfigPath)
	tool.ToFlagStringVar("sasl.realm", "Kerberos realm", "", &opts.Realm)
	tool.ToFlagStringVar("sasl.kerberos-auth-type", "Kerberos auth type. Either 'keytabAuth' or 'userAuth'", "", &opts.KerberosAuthType)
	tool.ToFlagStringVar("sasl.keytab-path", "Kerberos keytab file path", "", &opts.KeyTabPath)
	tool.ToFlagBoolVar("sasl.disable-PA-FX-FAST", "Configure the Kerberos client to not use PA_FX_FAST, default is false.", false, "false", &opts.SaslDisablePAFXFast)
	tool.ToFlagBoolVar("tls.enabled", "Connect to Kafka using TLS, default is false.", false, "false", &opts.UseTLS)
	tool.ToFlagStringVar("tls.server-name", "Used to verify the hostname on the returned certificates unless tls.insecure-skip-tls-verify is given. The kafka server's name should be given.", "", &opts.TlsServerName)
	tool.ToFlagStringVar("tls.ca-file", "The optional certificate authority file for Kafka TLS client authentication.", "", &opts.TlsCAFile)
	tool.ToFlagStringVar("tls.cert-file", "The optional certificate file for Kafka client authentication.", "", &opts.TlsCertFile)
	tool.ToFlagStringVar("tls.key-file", "The optional key file for Kafka client authentication.", "", &opts.TlsKeyFile)
	tool.ToFlagBoolVar("server.tls.enabled", "Enable TLS for web server, default is false.", false, "false", &opts.ServerUseTLS)
	tool.ToFlagBoolVar("server.tls.mutual-auth-enabled", "Enable TLS client mutual authentication, default is false.", false, "false", &opts.ServerMutualAuthEnabled)
	tool.ToFlagStringVar("server.tls.ca-file", "The certificate authority file for the web server.", "", &opts.ServerTlsCAFile)
	tool.ToFlagStringVar("server.tls.cert-file", "The certificate file for the web server.", "", &opts.ServerTlsCertFile)
	tool.ToFlagStringVar("server.tls.key-file", "The key file for the web server.", "", &opts.ServerTlsKeyFile)
	tool.ToFlagBoolVar("tls.insecure-skip-tls-verify", "If true, the server's certificate will not be checked for validity. This will make your HTTPS connections insecure. Default is false", false, "false", &opts.TlsInsecureSkipTLSVerify)
	tool.ToFlagStringVar("kafka.version", "Kafka broker version", sarama.V2_0_0_0.String(), &opts.KafkaVersion)
	tool.ToFlagBoolVar("use.consumelag.zookeeper", "if you need to use a group from zookeeper, default is false", false, "false", &opts.UseZooKeeperLag)
	tool.ToFlagStringsVar("zookeeper.server", "Address (hosts) of zookeeper server.", "localhost:2181", &opts.UriZookeeper)
	tool.ToFlagStringVar("kafka.labels", "Kafka cluster name", "", &opts.Labels)
	tool.ToFlagStringVar("refresh.metadata", "Metadata refresh interval", "30s", &opts.MetadataRefreshInterval)
	tool.ToFlagBoolVar("offset.show-all", "Whether show the offset/lag for all consumer group, otherwise, only show connected consumer groups, default is true", true, "true", &opts.OffsetShowAll)
	tool.ToFlagBoolVar("concurrent.enable", "If true, all scrapes will trigger kafka operations otherwise, they will share results. WARN: This should be disabled on large clusters. Default is false", false, "false", &opts.AllowConcurrent)
	tool.ToFlagIntVar("topic.workers", "Number of topic workers", 100, "100", &opts.TopicWorkers)
	tool.ToFlagBoolVar("kafka.allow-auto-topic-creation", "If true, the broker may auto-create topics that we requested which do not already exist, default is false.", false, "false", &opts.AllowAutoTopicCreation)
	tool.ToFlagIntVar("verbosity", "Verbosity log level", 0, "0", &opts.VerbosityLogLevel)

	plConfig := plog.Config{}
	plogflag.AddFlags(kingpin.CommandLine, &plConfig)
	kingpin.Version(version.Print("kafka_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	labels := make(map[string]string)

	// Protect against empty labels
	if opts.Labels != "" {
		for _, label := range strings.Split(opts.Labels, ",") {
			splitted := strings.Split(label, "=")
			if len(splitted) >= 2 {
				labels[splitted[0]] = splitted[1]
			}
		}
	}

	exporter.Setup(*listenAddress, *metricsPath, *topicFilter, *topicExclude, *groupFilter, *groupExclude, *logSarama, opts, labels)
}
