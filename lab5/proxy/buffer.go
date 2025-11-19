package proxy

const (
	buffer_size = 1 << 15
)

type BufferProvider interface {
	Get() []byte
	Put([]byte)
}

type AllocProvider struct {
	size int
}

func NewAllocProvider(size int) *AllocProvider {
	if size <= 0 {
		size = buffer_size
	}
	return &AllocProvider{size: size}
}

func (p *AllocProvider) Get() []byte {
	return make([]byte, p.size)
}

func (p *AllocProvider) Put(b []byte) {
	_ = b
}
