package exporter

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"github.com/Shopify/sarama"
	"github.com/krallistic/kazoo-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"io/ioutil"
	"k8s.io/klog/v2"
	"kafka_exporter/tool"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CanReadCertAndKey returns true if the certificate and key files already exists,
// otherwise returns false. If lost one of cert and key, returns error.
// 这个函数检查给定的证书和密钥文件是否存在且可读。
func CanReadCertAndKey(certPath, keyPath string) (bool, error) {
	certReadable := canReadFile(certPath)
	keyReadable := canReadFile(keyPath)

	if certReadable == false && keyReadable == false {
		return false, nil
	}

	if certReadable == false {
		return false, fmt.Errorf("error reading %s, certificate and key must be supplied as a pair", certPath)
	}

	if keyReadable == false {
		return false, fmt.Errorf("error reading %s, certificate and key must be supplied as a pair", keyPath)
	}

	return true, nil
}

// If the file represented by path exists and
// readable, returns true otherwise returns false.
// 这个函数检查给定的文件是否存在且可读。
func canReadFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}

	defer f.Close()

	return true
}

// NewExporter returns an initialized Exporter.
// 构造函数，用于创建并初始化一个 Exporter 对象。它接收一系列参数，包括 Kafka 配置选项、主题过滤器、主题排除器、组过滤器和组排除器。
func NewExporter(opts KafkaOpts, topicFilter string, topicExclude string, groupFilter string, groupExclude string) (*Exporter, error) {
	var zookeeperClient *kazoo.Kazoo
	config := sarama.NewConfig()
	config.ClientID = clientID
	//创建了一个新的 Sarama 配置对象，并设置了客户端 ID 和 Kafka 版本。
	kafkaVersion, err := sarama.ParseKafkaVersion(opts.KafkaVersion)
	if err != nil {
		return nil, err
	}
	config.Version = kafkaVersion

	//如果启用了 SASL（Simple Authentication and Security Layer，简单认证和安全层），
	//则配置 SASL 相关的设置，包括 SASL 机制（如 SCRAM-SHA512、SCRAM-SHA256、GSSAPI 或 PLAIN）、用户名和密码等。
	if opts.UseSASL {
		// Convert to lowercase so that SHA512 and SHA256 is still valid
		opts.SaslMechanism = strings.ToLower(opts.SaslMechanism)
		switch opts.SaslMechanism {
		case "scram-sha512":
			config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient { return &tool.XDGSCRAMClient{HashGeneratorFcn: tool.SHA512} }
			config.Net.SASL.Mechanism = sarama.SASLMechanism(sarama.SASLTypeSCRAMSHA512)
		case "scram-sha256":
			config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient { return &tool.XDGSCRAMClient{HashGeneratorFcn: tool.SHA256} }
			config.Net.SASL.Mechanism = sarama.SASLMechanism(sarama.SASLTypeSCRAMSHA256)
		case "gssapi":
			config.Net.SASL.Mechanism = sarama.SASLMechanism(sarama.SASLTypeGSSAPI)
			config.Net.SASL.GSSAPI.ServiceName = opts.ServiceName
			config.Net.SASL.GSSAPI.KerberosConfigPath = opts.KerberosConfigPath
			config.Net.SASL.GSSAPI.Realm = opts.Realm
			config.Net.SASL.GSSAPI.Username = opts.SaslUsername
			if opts.KerberosAuthType == "keytabAuth" {
				config.Net.SASL.GSSAPI.AuthType = sarama.KRB5_KEYTAB_AUTH
				config.Net.SASL.GSSAPI.KeyTabPath = opts.KeyTabPath
			} else {
				config.Net.SASL.GSSAPI.AuthType = sarama.KRB5_USER_AUTH
				config.Net.SASL.GSSAPI.Password = opts.SaslPassword
			}
			if opts.SaslDisablePAFXFast {
				config.Net.SASL.GSSAPI.DisablePAFXFAST = true
			}
		case "plain":
		default:
			return nil, fmt.Errorf(
				`invalid sasl mechanism "%s": can only be "scram-sha256", "scram-sha512", "gssapi" or "plain"`,
				opts.SaslMechanism,
			)
		}

		config.Net.SASL.Enable = true
		config.Net.SASL.Handshake = opts.UseSASLHandshake

		if opts.SaslUsername != "" {
			config.Net.SASL.User = opts.SaslUsername
		}

		if opts.SaslPassword != "" {
			config.Net.SASL.Password = opts.SaslPassword
		}
	}

	//如果启用了 TLS（Transport Layer Security，传输层安全协议），
	//则配置 TLS 相关的设置，包括是否启用 TLS、TLS 配置（如服务器名称、是否跳过证书验证）、CA 文件、证书文件和密钥文件等。
	if opts.UseTLS {
		config.Net.TLS.Enable = true

		config.Net.TLS.Config = &tls.Config{
			ServerName:         opts.TlsServerName,
			InsecureSkipVerify: opts.TlsInsecureSkipTLSVerify,
		}

		if opts.TlsCAFile != "" {
			if ca, err := ioutil.ReadFile(opts.TlsCAFile); err == nil {
				config.Net.TLS.Config.RootCAs = x509.NewCertPool()
				config.Net.TLS.Config.RootCAs.AppendCertsFromPEM(ca)
			} else {
				return nil, err
			}
		}

		canReadCertAndKey, err := CanReadCertAndKey(opts.TlsCertFile, opts.TlsKeyFile)
		if err != nil {
			return nil, errors.Wrap(err, "error reading cert and key")
		}
		if canReadCertAndKey {
			cert, err := tls.LoadX509KeyPair(opts.TlsCertFile, opts.TlsKeyFile)
			if err == nil {
				config.Net.TLS.Config.Certificates = []tls.Certificate{cert}
			} else {
				return nil, err
			}
		}
	}

	//如果启用了 ZooKeeper 延迟，那么就创建一个新的 ZooKeeper 客户端。
	if opts.UseZooKeeperLag {
		klog.V(DEBUG).Infoln("Using zookeeper lag, so connecting to zookeeper")
		zookeeperClient, err = kazoo.NewKazoo(opts.UriZookeeper, nil)
		if err != nil {
			return nil, errors.Wrap(err, "error connecting to zookeeper")
		}
	}

	//解析并设置元数据刷新间隔。
	interval, err := time.ParseDuration(opts.MetadataRefreshInterval)
	if err != nil {
		return nil, errors.Wrap(err, "Cannot parse metadata refresh interval")
	}

	config.Metadata.RefreshFrequency = interval

	config.Metadata.AllowAutoTopicCreation = opts.AllowAutoTopicCreation

	//创建一个新的 Sarama 客户端。
	client, err := sarama.NewClient(opts.Uri, config)

	if err != nil {
		return nil, errors.Wrap(err, "Error Init Kafka Client")
	}

	klog.V(TRACE).Infoln("Done Init Clients")
	// Init our exporter.
	//创建并返回一个新的 Exporter 对象，其中包含了 Kafka 客户端、主题过滤器、主题排除器、组过滤器、
	//组排除器、ZooKeeper 客户端、元数据刷新间隔、是否显示所有偏移量、主题工作器数量、是否允许并发等信息。
	return &Exporter{
		client:                  client,
		topicFilter:             regexp.MustCompile(topicFilter),
		topicExclude:            regexp.MustCompile(topicExclude),
		groupFilter:             regexp.MustCompile(groupFilter),
		groupExclude:            regexp.MustCompile(groupExclude),
		useZooKeeperLag:         opts.UseZooKeeperLag,
		zookeeperClient:         zookeeperClient,
		nextMetadataRefresh:     time.Now(),
		metadataRefreshInterval: interval,
		offsetShowAll:           opts.OffsetShowAll,
		topicWorkers:            opts.TopicWorkers,
		allowConcurrent:         opts.AllowConcurrent,
		sgMutex:                 sync.Mutex{},
		sgWaitCh:                nil,
		sgChans:                 []chan<- prometheus.Metric{},
		consumerGroupFetchAll:   config.Version.IsAtLeast(sarama.V2_0_0_0),
	}, nil
}

// 初始化 Kafka exporter 并启动 HTTP 服务，让 Prometheus 可以从这个服务中抓取 Kafka 的指标。这个函数通常在程序的入口点（main 函数）调用。
func Setup(
	listenAddress string,
	metricsPath string,
	topicFilter string,
	topicExclude string,
	groupFilter string,
	groupExclude string,
	logSarama bool,
	opts KafkaOpts,
	labels map[string]string,
) {
	klog.InitFlags(flag.CommandLine)
	if err := flag.Set("logtostderr", "true"); err != nil {
		klog.Errorf("Error on setting logtostderr to true: %v", err)
	}
	err := flag.Set("v", strconv.Itoa(opts.VerbosityLogLevel))
	if err != nil {
		klog.Errorf("Error on setting v to %v: %v", strconv.Itoa(opts.VerbosityLogLevel), err)
	}
	defer klog.Flush()

	klog.V(INFO).Infoln("Starting kafka_exporter", version.Info())
	klog.V(DEBUG).Infoln("Build context", version.BuildContext())

	//创建了一系列的 Prometheus 指标描述，这些描述定义了 Kafka exporter 将要暴露的指标的名称、帮助信息、标签等信息。
	clusterBrokers = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "brokers"),
		"Number of Brokers in the Kafka Cluster.",
		nil, labels,
	)
	clusterBrokerInfo = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "broker_info"),
		"Information about the Kafka Broker.",
		[]string{"id", "address"}, labels,
	)
	topicPartitions = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "topic", "partitions"),
		"Number of partitions for this Topic",
		[]string{"topic"}, labels,
	)
	topicCurrentOffset = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "topic", "partition_current_offset"),
		"Current Offset of a Broker at Topic/Partition",
		[]string{"topic", "partition"}, labels,
	)
	topicOldestOffset = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "topic", "partition_oldest_offset"),
		"Oldest Offset of a Broker at Topic/Partition",
		[]string{"topic", "partition"}, labels,
	)

	topicPartitionLeader = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "topic", "partition_leader"),
		"Leader Broker ID of this Topic/Partition",
		[]string{"topic", "partition"}, labels,
	)

	topicPartitionReplicas = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "topic", "partition_replicas"),
		"Number of Replicas for this Topic/Partition",
		[]string{"topic", "partition"}, labels,
	)

	topicPartitionInSyncReplicas = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "topic", "partition_in_sync_replica"),
		"Number of In-Sync Replicas for this Topic/Partition",
		[]string{"topic", "partition"}, labels,
	)

	topicPartitionUsesPreferredReplica = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "topic", "partition_leader_is_preferred"),
		"1 if Topic/Partition is using the Preferred Broker",
		[]string{"topic", "partition"}, labels,
	)

	topicUnderReplicatedPartition = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "topic", "partition_under_replicated_partition"),
		"1 if Topic/Partition is under Replicated",
		[]string{"topic", "partition"}, labels,
	)

	consumergroupCurrentOffset = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "consumergroup", "current_offset"),
		"Current Offset of a ConsumerGroup at Topic/Partition",
		[]string{"consumergroup", "topic", "partition"}, labels,
	)

	consumergroupCurrentOffsetSum = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "consumergroup", "current_offset_sum"),
		"Current Offset of a ConsumerGroup at Topic for all partitions",
		[]string{"consumergroup", "topic"}, labels,
	)

	consumergroupLag = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "consumergroup", "lag"),
		"Current Approximate Lag of a ConsumerGroup at Topic/Partition",
		[]string{"consumergroup", "topic", "partition"}, labels,
	)

	consumergroupLagZookeeper = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "consumergroupzookeeper", "lag_zookeeper"),
		"Current Approximate Lag(zookeeper) of a ConsumerGroup at Topic/Partition",
		[]string{"consumergroup", "topic", "partition"}, nil,
	)

	consumergroupLagSum = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "consumergroup", "lag_sum"),
		"Current Approximate Lag of a ConsumerGroup at Topic for all partitions",
		[]string{"consumergroup", "topic"}, labels,
	)

	consumergroupMembers = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "consumergroup", "members"),
		"Amount of members in a consumer group",
		[]string{"consumergroup"}, labels,
	)

	if logSarama {
		sarama.Logger = log.New(os.Stdout, "[sarama] ", log.LstdFlags)
	}

	//使用 NewExporter 函数创建了一个 Kafka exporter，这个 exporter 会连接到 Kafka 集群并收集指标。
	exporter, err := NewExporter(opts, topicFilter, topicExclude, groupFilter, groupExclude)
	if err != nil {
		klog.Fatalln(err)
	}
	defer exporter.client.Close()
	//将 Kafka exporter 注册到 Prometheus，这样 Prometheus 就可以从 Kafka exporter 中抓取指标。
	prometheus.MustRegister(exporter)

	http.Handle(metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(`<html>
	        <head><title>Kafka Exporter</title></head>
	        <body>
	        <h1>Kafka Exporter</h1>
	        <p><a href='` + metricsPath + `'>Metrics</a></p>
	        </body>
	        </html>`))
		if err != nil {
			klog.Error("Error handle / request", err)
		}
	})
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		// need more specific sarama check
		_, err := w.Write([]byte("ok"))
		if err != nil {
			klog.Error("Error handle /healthz request", err)
		}
	})

	if opts.ServerUseTLS {
		klog.V(INFO).Infoln("Listening on HTTPS", listenAddress)

		_, err := CanReadCertAndKey(opts.ServerTlsCertFile, opts.ServerTlsKeyFile)
		if err != nil {
			klog.Error("error reading server cert and key")
		}

		clientAuthType := tls.NoClientCert
		if opts.ServerMutualAuthEnabled {
			clientAuthType = tls.RequireAndVerifyClientCert
		}

		certPool := x509.NewCertPool()
		if opts.ServerTlsCAFile != "" {
			if caCert, err := ioutil.ReadFile(opts.ServerTlsCAFile); err == nil {
				certPool.AppendCertsFromPEM(caCert)
			} else {
				klog.Error("error reading server ca")
			}
		}

		tlsConfig := &tls.Config{
			ClientCAs:                certPool,
			ClientAuth:               clientAuthType,
			MinVersion:               tls.VersionTLS12,
			CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
			PreferServerCipherSuites: true,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
				tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
				tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_RSA_WITH_AES_256_CBC_SHA,
				tls.TLS_RSA_WITH_AES_128_CBC_SHA256,
			},
		}
		server := &http.Server{
			Addr:      listenAddress,
			TLSConfig: tlsConfig,
		}
		klog.Fatal(server.ListenAndServeTLS(opts.ServerTlsCertFile, opts.ServerTlsKeyFile))
	} else {
		klog.V(INFO).Infoln("Listening on HTTP", listenAddress)
		klog.Fatal(http.ListenAndServe(listenAddress, nil))
	}
}
