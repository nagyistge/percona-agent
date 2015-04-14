package mock

type Queue []string

func (q *Queue) Push(s string) {
	*q = append(*q, s)
}

func (q *Queue) Pop() (s string) {
	s = (*q)[0]
	*q = (*q)[1:]
	return
}

func (q *Queue) Len() int {
	return len(*q)
}

type FakeUUID4 struct {
	fakeIDs *Queue
}

func (u *FakeUUID4) New() string {
	return u.fakeIDs.Pop()
}

func (u *FakeUUID4) Append(s string) {
	u.fakeIDs.Push(s)
}

func NewFakeUUID4(values []string) *FakeUUID4 {
	queue := make(Queue, 0)
	for i := len(values) - 1; i >= 0; i-- {
		queue.Push(values[i])
	}
	return &FakeUUID4{fakeIDs: &queue}
}
