package kafkafixture

import "github.com/IBM/sarama"

func Run(producer sarama.SyncProducer, consumer sarama.Consumer) {
	producer.SendMessage(&sarama.ProducerMessage{Topic: "orders"})
	consumer.ConsumePartition("payments", 0, 0)
}
