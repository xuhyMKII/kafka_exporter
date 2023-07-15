
# Kafka Exporter

Kafka Exporter is a Prometheus exporter for Apache Kafka, which provides Kafka cluster metrics. It uses the Sarama library to connect to Kafka clusters and fetch metrics.

## Features

-   Fetches metrics from Kafka brokers
-   Fetches consumer group metrics
-   Fetches topic metrics
-   Supports Kafka version 0.10.1.0 and later

## Prerequisites

-   Go 1.16 or later
-   Docker (for building Docker images)

## Building

To build the Kafka Exporter from source, you can use the provided Makefile:

bashCopy code

`make build`

This will compile the Kafka Exporter and output a binary named `kafka_exporter`.

To build a Docker image, you can use:

bashCopy code

`make docker`

## Running

You can run the Kafka Exporter directly from the command line:

bashCopy code

`./kafka_exporter --kafka.server=kafka:9092`

Replace `kafka:9092` with the address of your Kafka server.

You can also run the Kafka Exporter as a Docker container:

bashCopy code

`docker run -p 9308:9308 danielqsj/kafka-exporter --kafka.server=kafka:9092`

## Configuration

Kafka Exporter supports a number of configuration options, which can be provided as command-line arguments. Here are some of the most important ones:

-   `--kafka.server`: Address (host:port) of the Kafka server to connect to.
-   `--kafka.version`: Kafka broker version.
-   `--sasl.enabled`: Use SASL/PLAIN authentication.
-   `--sasl.username`: SASL user name.
-   `--sasl.password`: SASL user password.
-   `--tls.enabled`: Use TLS to connect to the broker.
-   `--tls.ca-file`: Path to the CA file.
-   `--tls.cert-file`: Path to the TLS certificate file.
-   `--tls.key-file`: Path to the TLS key file.

For a full list of configuration options, run:

bashCopy code

`./kafka_exporter --help`

## Metrics

Kafka Exporter provides a wide range of metrics, including:

-   Broker information
-   Topic partition count
-   Current and oldest offsets for each topic partition
-   Leader information for each topic partition
-   Replica count for each topic partition
-   In-sync replica count for each topic partition
-   Whether each topic partition is using the preferred leader
-   Whether each topic partition is under-replicated
-   Current offset and lag for each consumer group

Metrics are exposed on port 9308 by default, and can be scraped by Prometheus.

## Logging

Kafka Exporter uses the `klog` library for logging. You can control the log level with the `-v` command-line option. For example, `-v=2` sets the log level to 2.


## License

Kafka Exporter is licensed under the Apache License, Version 2.0. See the [LICENSE](https://chat.openai.com/c/LICENSE) file for details.