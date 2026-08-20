package deploys

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestContainsBSONString(t *testing.T) {
	tests := []struct {
		name   string
		value  interface{}
		target string
		want   bool
	}{
		{name: "bson array contains target", value: bson.A{"one", "two"}, target: "two", want: true},
		{name: "string slice contains target", value: []string{"one"}, target: "one", want: true},
		{name: "target absent", value: bson.A{"one"}, target: "two", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := containsBSONString(test.value, test.target); got != test.want {
				t.Fatalf("containsBSONString() = %v, want %v", got, test.want)
			}
		})
	}
}
