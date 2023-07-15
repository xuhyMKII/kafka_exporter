package exporter

import (
	"github.com/Shopify/sarama"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog/v2"
	"strconv"
	"sync"
	"time"
)

//这段代码是kafka_exporter开始抓取数据的部分。这段代码的主要目的是处理并发的Collect调用。
//
//这段代码首先获取一个互斥锁，以确保对e.sgChans的操作是线程安全的。然后，它将当前的Collect调用的channel添加到e.sgChans中。
//
//接下来，它检查e.sgChans的长度。如果长度为1，那么这意味着这是第一个Collect调用，它会创建一个新的channel e.sgWaitCh，
//并启动一个新的goroutine来抓取数据。这个goroutine会调用e.collectChans(e.sgWaitCh)，这个函数会从Kafka抓取数据，
//并将数据转换为Prometheus的指标，然后将这些指标发送到e.sgChans中的所有channel。
//
//如果e.sgChans的长度大于1，那么这意味着已经有一个Collect调用正在进行，
//这个Collect调用会打印一条日志，然后等待第一个Collect调用完成。
//
//最后，这段代码将e.sgWaitCh的值保存到一个新的变量waiter中，然后释放互斥锁。
//这是为了防止在等待第一个Collect调用完成的过程中，e.sgWaitCh的值被另一个Collect调用修改。
//
//总的来说，这段代码的目的是确保并发的Collect调用可以正确地处理，并且所有的Collect调用都可以获取到最新的数据。

// Collect fetches the stats from configured Kafka location and delivers them
// as Prometheus metrics. It implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	if e.allowConcurrent {
		e.collect(ch)
		return
	}
	// Locking to avoid race add
	e.sgMutex.Lock()
	e.sgChans = append(e.sgChans, ch)
	// Safe to compare length since we own the Lock
	if len(e.sgChans) == 1 {
		e.sgWaitCh = make(chan struct{})
		go e.collectChans(e.sgWaitCh)
	} else {
		klog.V(TRACE).Info("concurrent calls detected, waiting for first to finish")
	}
	// Put in another variable to ensure not overwriting it in another Collect once we wait
	waiter := e.sgWaitCh
	e.sgMutex.Unlock()
	// Released lock, we have insurance that our chan will be part of the collectChan slice
	<-waiter
	// collectChan finished
}

// 抓取Kafka的数据，将数据转换为Prometheus的指标，并将这些指标发送到所有等待的Collect调用。
func (e *Exporter) collectChans(quit chan struct{}) {
	//创建一个新的prometheus.Metric类型的channel，用于存储抓取到的数据转换后的Prometheus指标。
	original := make(chan prometheus.Metric)
	//创建一个prometheus.Metric类型的切片，用于存储从original channel中读取到的所有指标。
	container := make([]prometheus.Metric, 0, 100)
	//启动一个新的goroutine，这个goroutine会从original channel中读取指标，并将这些指标添加到container切片中。
	go func() {
		for metric := range original {
			container = append(container, metric)
		}
	}()
	//调用collect方法抓取Kafka的数据，将数据转换为Prometheus的指标，并将这些指标发送到original channel。
	e.collect(original)
	//关闭original channel，这会导致上面的goroutine退出。
	close(original)
	// Lock to avoid modification on the channel slice 获取互斥锁，以确保对e.sgChans的操作是线程安全的。
	e.sgMutex.Lock()
	//遍历e.sgChans中的所有channel，将container切片中的所有指标发送到这些channel。
	for _, ch := range e.sgChans {
		for _, metric := range container {
			ch <- metric
		}
	}
	// Reset the slice 重置e.sgChans切片，以便于下一次的Collect调用。
	e.sgChans = e.sgChans[:0]
	// Notify remaining waiting Collect they can return
	//关闭quit channel，这会导致所有等待的Collect调用返回。
	close(quit)
	// Release the lock so Collect can append to the slice again
	//释放互斥锁，这样其他的Collect调用就可以再次添加channel到e.sgChans了。
	e.sgMutex.Unlock()
}

func (e *Exporter) collect(ch chan<- prometheus.Metric) {

	//创建一个新的sync.WaitGroup，用于等待所有的goroutine完成。
	var wg = sync.WaitGroup{}

	//创建了一个名为clusterBrokers的指标，这个指标的类型是Gauge（即可以任意变高变低的指标），
	//值是当前Kafka集群中broker的数量。然后将这个指标发送到channel。
	ch <- prometheus.MustNewConstMetric(
		clusterBrokers, prometheus.GaugeValue, float64(len(e.client.Brokers())),
	)

	//遍历当前Kafka集群中的所有broker，对每个broker创建一个名为clusterBrokerInfo的指标，
	//这个指标的类型也是Gauge，值是1（表示这个broker存在），并且这个指标带有两个标签，
	//分别是broker的ID和地址。然后将这个指标发送到channel。
	for _, b := range e.client.Brokers() {
		ch <- prometheus.MustNewConstMetric(
			clusterBrokerInfo, prometheus.GaugeValue, 1, strconv.Itoa(int(b.ID())), b.Addr(),
		)
	}

	//创建一个新的map，用于存储每个topic的每个partition的offset。
	offset := make(map[string]map[int32]int64)

	now := time.Now()

	//如果当前的时间在下一次元数据刷新的时间之后，那么刷新Kafka的元数据，并更新下一次元数据刷新的时间。
	if now.After(e.nextMetadataRefresh) {
		klog.V(DEBUG).Info("Refreshing client metadata")

		if err := e.client.RefreshMetadata(); err != nil {
			klog.Errorf("Cannot refresh topics, using cached data: %v", err)
		}

		e.nextMetadataRefresh = now.Add(e.metadataRefreshInterval)
	}

	//获取Kafka的所有topic。
	topics, err := e.client.Topics()
	if err != nil {
		klog.Errorf("Cannot get topics: %v", err)
		return
	}

	//创建一个新的channel，用于存储需要获取指标的topic。
	topicChannel := make(chan string)

	loopTopics := func() {
		ok := true
		for ok {
			topic, open := <-topicChannel
			ok = open
			if open {
				e.getTopicMetrics(topic, ch, &wg, offset)
			}
		}
	}

	minx := func(x int, y int) int {
		if x < y {
			return x
		} else {
			return y
		}
	}

	N := len(topics)
	if N > 1 {
		N = minx(N/2, e.topicWorkers)
	}

	for w := 1; w <= N; w++ {
		go loopTopics()
	}

	for _, topic := range topics {
		if e.topicFilter.MatchString(topic) && !e.topicExclude.MatchString(topic) {
			wg.Add(1)
			topicChannel <- topic
		}
	}
	close(topicChannel)

	wg.Wait()

	klog.V(DEBUG).Info("Fetching consumer group metrics")
	if len(e.client.Brokers()) > 0 {
		for _, broker := range e.client.Brokers() {
			wg.Add(1)
			go e.getConsumerGroupMetrics(broker, ch, &wg, offset)
		}
		wg.Wait()
	} else {
		klog.Errorln("No valid broker, cannot get consumer group metrics")
	}
}

// 定义一个新的函数，用于获取指定topic的指标。这个函数会获取topic的所有partition的指标，
// 包括leader、当前offset、最旧offset、副本数、同步副本数、是否使用首选副本、是否有副本未同步等。
func (e *Exporter) getTopicMetrics(topic string, ch chan<- prometheus.Metric, wg *sync.WaitGroup, offset map[string]map[int32]int64) {
	defer wg.Done()

	//检查当前主题是否满足过滤条件。如果主题不满足topicFilter或者满足topicExclude，函数就会直接返回，不会继续处理这个主题。
	if !e.topicFilter.MatchString(topic) || e.topicExclude.MatchString(topic) {
		return
	}

	//函数会获取当前主题的所有分区（partition）。如果无法获取分区，函数会记录错误并返回。
	//如果成功获取到分区，函数会创建一个名为topicPartitions的指标，这个指标的值是分区的数量，并将这个指标发送到channel。
	partitions, err := e.client.Partitions(topic)
	if err != nil {
		klog.Errorf("Cannot get partitions of topic %s: %v", topic, err)
		return
	}
	ch <- prometheus.MustNewConstMetric(
		topicPartitions, prometheus.GaugeValue, float64(len(partitions)), topic,
	)
	e.mu.Lock()
	offset[topic] = make(map[int32]int64, len(partitions))
	e.mu.Unlock()

	//遍历当前主题的所有分区
	for _, partition := range partitions {

		//获取分区的leader broker，并创建一个名为topicPartitionLeader的指标，这个指标的值是leader broker的ID，并将这个指标发送到channel。
		broker, err := e.client.Leader(topic, partition)
		if err != nil {
			klog.Errorf("Cannot get leader of topic %s partition %d: %v", topic, partition, err)
		} else {
			ch <- prometheus.MustNewConstMetric(
				topicPartitionLeader, prometheus.GaugeValue, float64(broker.ID()), topic, strconv.FormatInt(int64(partition), 10),
			)
		}

		//获取分区的当前offset（即最新的消息的offset），并创建一个名为topicCurrentOffset的指标，这个指标的值是当前offset，并将这个指标发送到channel。
		currentOffset, err := e.client.GetOffset(topic, partition, sarama.OffsetNewest)
		if err != nil {
			klog.Errorf("Cannot get current offset of topic %s partition %d: %v", topic, partition, err)
		} else {
			e.mu.Lock()
			offset[topic][partition] = currentOffset
			e.mu.Unlock()
			ch <- prometheus.MustNewConstMetric(
				topicCurrentOffset, prometheus.GaugeValue, float64(currentOffset), topic, strconv.FormatInt(int64(partition), 10),
			)
		}

		//获取分区的最旧offset（即最早的消息的offset），并创建一个名为topicOldestOffset的指标，这个指标的值是最旧offset，并将这个指标发送到channel。
		oldestOffset, err := e.client.GetOffset(topic, partition, sarama.OffsetOldest)
		if err != nil {
			klog.Errorf("Cannot get oldest offset of topic %s partition %d: %v", topic, partition, err)
		} else {
			ch <- prometheus.MustNewConstMetric(
				topicOldestOffset, prometheus.GaugeValue, float64(oldestOffset), topic, strconv.FormatInt(int64(partition), 10),
			)
		}

		//获取分区的所有副本（replica），并创建一个名为topicPartitionReplicas的指标，这个指标的值是副本的数量，并将这个指标发送到channel。
		replicas, err := e.client.Replicas(topic, partition)
		if err != nil {
			klog.Errorf("Cannot get replicas of topic %s partition %d: %v", topic, partition, err)
		} else {
			ch <- prometheus.MustNewConstMetric(
				topicPartitionReplicas, prometheus.GaugeValue, float64(len(replicas)), topic, strconv.FormatInt(int64(partition), 10),
			)
		}

		//获取分区的所有同步副本（in-sync replica），并创建一个名为topicPartitionInSyncReplicas的指标，这个指标的值是同步副本的数量，并将这个指标发送到channel。
		inSyncReplicas, err := e.client.InSyncReplicas(topic, partition)
		if err != nil {
			klog.Errorf("Cannot get in-sync replicas of topic %s partition %d: %v", topic, partition, err)
		} else {
			ch <- prometheus.MustNewConstMetric(
				topicPartitionInSyncReplicas, prometheus.GaugeValue, float64(len(inSyncReplicas)), topic, strconv.FormatInt(int64(partition), 10),
			)
		}

		//检查分区是否使用了首选副本（preferred replica），并创建一个名为topicPartitionUsesPreferredReplica的指标，
		//这个指标的值是1或0，表示是否使用了首选副本，并将这个指标发送到channel。
		if broker != nil && replicas != nil && len(replicas) > 0 && broker.ID() == replicas[0] {
			ch <- prometheus.MustNewConstMetric(
				topicPartitionUsesPreferredReplica, prometheus.GaugeValue, float64(1), topic, strconv.FormatInt(int64(partition), 10),
			)
		} else {
			ch <- prometheus.MustNewConstMetric(
				topicPartitionUsesPreferredReplica, prometheus.GaugeValue, float64(0), topic, strconv.FormatInt(int64(partition), 10),
			)
		}

		//检查分区是否处于欠复制状态（under-replicated），并创建一个名为topicUnderReplicatedPartition的指标，
		//这个指标的值是1或0，表示分区是否处于欠复制状态，并将这个指标发送到channel。
		if replicas != nil && inSyncReplicas != nil && len(inSyncReplicas) < len(replicas) {
			ch <- prometheus.MustNewConstMetric(
				topicUnderReplicatedPartition, prometheus.GaugeValue, float64(1), topic, strconv.FormatInt(int64(partition), 10),
			)
		} else {
			ch <- prometheus.MustNewConstMetric(
				topicUnderReplicatedPartition, prometheus.GaugeValue, float64(0), topic, strconv.FormatInt(int64(partition), 10),
			)
		}

		//如果启用了ZooKeeper延迟检查，函数会获取所有消费者组（consumer group）的offset，
		//计算出消费者组的延迟（lag），并创建一个名为consumergroupLagZookeeper的指标，
		//这个指标的值是消费者组的延迟，并将这个指标发送到channel。
		if e.useZooKeeperLag {
			ConsumerGroups, err := e.zookeeperClient.Consumergroups()

			if err != nil {
				klog.Errorf("Cannot get consumer group %v", err)
			}

			for _, group := range ConsumerGroups {
				offset, _ := group.FetchOffset(topic, partition)
				if offset > 0 {

					consumerGroupLag := currentOffset - offset
					ch <- prometheus.MustNewConstMetric(
						consumergroupLagZookeeper, prometheus.GaugeValue, float64(consumerGroupLag), group.Name, topic, strconv.FormatInt(int64(partition), 10),
					)
				}
			}
		}
	}
}

// 定义一个新的函数，用于获取指定broker的所有consumer group的指标。
// 这个函数会获取每个consumer group的每个topic的每个partition的offset，以及consumer group的成员数等。
func (e *Exporter) getConsumerGroupMetrics(broker *sarama.Broker, ch chan<- prometheus.Metric, wg *sync.WaitGroup, offset map[string]map[int32]int64) {
	defer wg.Done()
	//尝试打开到 Kafka broker 的连接。如果连接失败，函数会记录错误并返回。
	if err := broker.Open(e.client.Config()); err != nil && err != sarama.ErrAlreadyConnected {
		klog.Errorf("Cannot connect to broker %d: %v", broker.ID(), err)
		return
	}
	defer broker.Close()

	//发送一个 ListGroupsRequest 到 broker，以获取 Kafka 中所有的消费者组。
	groups, err := broker.ListGroups(&sarama.ListGroupsRequest{})
	if err != nil {
		klog.Errorf("Cannot get consumer group: %v", err)
		return
	}
	//每个消费者组，如果它的 ID 匹配了 groupFilter 并且没有被 groupExclude 排除，那么它的 ID 就会被添加到 groupIds 列表中。
	groupIds := make([]string, 0)
	for groupId := range groups.Groups {
		if e.groupFilter.MatchString(groupId) && !e.groupExclude.MatchString(groupId) {
			groupIds = append(groupIds, groupId)
		}
	}

	//发送一个 DescribeGroupsRequest 到 broker，以获取 groupIds 列表中的消费者组的详细信息。
	describeGroups, err := broker.DescribeGroups(&sarama.DescribeGroupsRequest{Groups: groupIds})
	if err != nil {
		klog.Errorf("Cannot get describe groups: %v", err)
		return
	}
	for _, group := range describeGroups.Groups {
		//对于每个消费者组，函数会创建一个 OffsetFetchRequest，并根据 offsetShowAll 的值来决定如何填充这个请求。
		offsetFetchRequest := sarama.OffsetFetchRequest{ConsumerGroup: group.GroupId, Version: 1}
		if e.offsetShowAll {
			//如果 offsetShowAll 为 true，那么函数会为 offset 中的每个主题和分区添加一个分区到请求中。
			for topic, partitions := range offset {
				for partition := range partitions {
					offsetFetchRequest.AddPartition(topic, partition)
				}
			}
		} else {
			//否则，函数会遍历消费者组的每个成员，获取他们的分配情况，并为每个分配的主题和分区添加一个分区到请求中。
			for _, member := range group.Members {
				assignment, err := member.GetMemberAssignment()
				if err != nil {
					klog.Errorf("Cannot get GetMemberAssignment of group member %v : %v", member, err)
					return
				}
				for topic, partions := range assignment.Topics {
					for _, partition := range partions {
						offsetFetchRequest.AddPartition(topic, partition)
					}
				}
			}
		}
		//创建一个新的 Prometheus 指标 consumergroupMembers，
		//这个指标的类型是 Gauge，值是消费者组的成员数量（len(group.Members)），
		//并且这个指标带有一个标签，标签的名字是消费者组的 ID（group.GroupId）。
		//然后，这个新创建的指标被发送到 ch 通道中。
		ch <- prometheus.MustNewConstMetric(
			consumergroupMembers, prometheus.GaugeValue, float64(len(group.Members)), group.GroupId,
		)

		//向 Kafka broker 发送一个 OffsetFetchRequest，请求获取消费者组的 offset。
		//OffsetFetchRequest 中包含了消费者组的 ID 和需要获取 offset 的主题和分区的列表。
		//broker 会返回一个 OffsetFetchResponse，其中包含了请求的 offset。
		offsetFetchResponse, err := broker.FetchOffset(&offsetFetchRequest)
		if err != nil {
			klog.Errorf("Cannot get offset of group %s: %v", group.GroupId, err)
			continue
		}

		//遍历每个消费者组消费的主题和分区，然后计算和记录每个主题和分区的当前 offset 和 lag。
		for topic, partitions := range offsetFetchResponse.Blocks {
			// If the topic is not consumed by that consumer group, skip it，标记当前主题是否被消费者组消费。
			topicConsumed := false
			//遍历每个分区的 offsetFetchResponseBlock。
			for _, offsetFetchResponseBlock := range partitions {
				// Kafka will return -1 if there is no offset associated with a topic-partition under that consumer group
				// 如果 offsetFetchResponseBlock 的 Offset 不等于 -1，说明这个主题被消费者组消费，设置 topicConsumed 为 true。
				if offsetFetchResponseBlock.Offset != -1 {
					topicConsumed = true
					break
				}
			}
			//如果主题没有被消费者组消费，那么跳过这个主题，处理下一个主题。
			//如果一个主题没有被消费者组消费，那么这个主题的 offset 和 lag 就没有意义，
			//因为没有消费者去读取和消费这个主题的数据。
			if !topicConsumed {
				continue
			}

			var currentOffsetSum int64
			var lagSum int64

			//遍历每个分区的 offsetFetchResponseBlock，计算和记录每个分区的当前 offset 和 lag。
			for partition, offsetFetchResponseBlock := range partitions {
				err := offsetFetchResponseBlock.Err
				if err != sarama.ErrNoError {
					klog.Errorf("Error for  partition %d :%v", partition, err.Error())
					continue
				}
				//获取当前分区的 offset。
				currentOffset := offsetFetchResponseBlock.Offset
				//累加当前主题的所有分区的 offset。
				currentOffsetSum += currentOffset

				//创建一个新的 Prometheus 指标 consumergroupCurrentOffset，
				//这个指标的类型是 Gauge，值是当前分区的 offset，标签包括消费者组的 ID、主题和分区。
				ch <- prometheus.MustNewConstMetric(
					consumergroupCurrentOffset, prometheus.GaugeValue, float64(currentOffset), group.GroupId, topic, strconv.FormatInt(int64(partition), 10),
				)

				e.mu.Lock()

				//
				//如果当前主题和分区的 offset 存在，那么计算 lag。
				//如果 offsetFetchResponseBlock 的 Offset 等于 -1，说明没有 offset，设置 lag 为 -1。
				//否则，lag 等于 offset 减去 offsetFetchResponseBlock 的 Offset
				if offset, ok := offset[topic][partition]; ok {
					// If the topic is consumed by that consumer group, but no offset associated with the partition
					// forcing lag to -1 to be able to alert on that
					var lag int64
					if offsetFetchResponseBlock.Offset == -1 {
						lag = -1
					} else {
						lag = offset - offsetFetchResponseBlock.Offset
						lagSum += lag
					}

					//创建一个新的 Prometheus 指标 consumergroupLag，这个指标的类型是 Gauge，值是 lag，标签包括消费者组的 ID、主题和分区。
					ch <- prometheus.MustNewConstMetric(
						consumergroupLag, prometheus.GaugeValue, float64(lag), group.GroupId, topic, strconv.FormatInt(int64(partition), 10),
					)
				} else {
					klog.Errorf("No offset of topic %s partition %d, cannot get consumer group lag", topic, partition)
				}
				e.mu.Unlock()
			}

			//创建两个新的 Prometheus 指标 consumergroupCurrentOffsetSum 和 consumergroupLagSum，
			//这两个指标的类型都是 Gauge，值分别是当前主题的所有分区的 offset 之和和 lag 之和，标签是消费者组的 ID 和主题。
			ch <- prometheus.MustNewConstMetric(
				consumergroupCurrentOffsetSum, prometheus.GaugeValue, float64(currentOffsetSum), group.GroupId, topic,
			)
			ch <- prometheus.MustNewConstMetric(
				consumergroupLagSum, prometheus.GaugeValue, float64(lagSum), group.GroupId, topic,
			)
		}
	}
}
