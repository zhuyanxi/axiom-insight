package sarama

type ProducerMessage struct {
	Topic string
}

type SyncProducer interface {
	SendMessage(*ProducerMessage) (int32, int64, error)
}

type Consumer interface {
	ConsumePartition(string, int32, int64) (PartitionConsumer, error)
}

type PartitionConsumer interface{}
