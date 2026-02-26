package shared

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// FindLatestPlatformCredential returns the most recently updated platform-scoped
// credential document for the provided collection.
func FindLatestPlatformCredential(ctx context.Context, collectionName string) (bson.M, error) {
	col := Collection(collectionName)
	filter := bson.M{"scope": "platform"}
	opts := options.FindOne().SetSort(bson.D{
		bson.E{Key: "updatedAt", Value: -1},
		bson.E{Key: "createdAt", Value: -1},
	})
	var result bson.M
	if err := col.FindOne(ctx, filter, opts).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}
