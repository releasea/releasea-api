package shared

import (
	"context"
	"errors"
	"log"
	"time"

	mongostore "releaseaapi/internal/platform/storage/mongo"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const DBTimeout = 15 * time.Second

func Collection(name string) *mongo.Collection {
	return mongostore.Mongo().Database(mongostore.DBName).Collection(name)
}

func FindAll(ctx context.Context, col *mongo.Collection, filter bson.M) ([]bson.M, error) {
	cursor, err := col.Find(ctx, filter)
	if err != nil {
		logDBError("findAll", col, err)
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil {
		logDBError("cursor.All", col, err)
		return nil, err
	}
	return results, nil
}

func FindAllSorted(ctx context.Context, col *mongo.Collection, filter bson.M, sort interface{}) ([]bson.M, error) {
	opts := options.Find().SetSort(sort)
	cursor, err := col.Find(ctx, filter, opts)
	if err != nil {
		logDBError("findAllSorted", col, err)
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil {
		logDBError("cursor.All", col, err)
		return nil, err
	}
	return results, nil
}

func FindOne(ctx context.Context, col *mongo.Collection, filter bson.M) (bson.M, error) {
	var result bson.M
	err := col.FindOne(ctx, filter).Decode(&result)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		logDBError("findOne", col, err)
	}
	return result, err
}

func InsertOne(ctx context.Context, col *mongo.Collection, doc bson.M) error {
	_, err := col.InsertOne(ctx, doc)
	if err != nil {
		logDBError("insertOne", col, err)
	}
	return err
}

func UpdateByID(ctx context.Context, col *mongo.Collection, id string, update bson.M) error {
	_, err := col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": update}, options.Update().SetUpsert(false))
	if err != nil {
		logDBError("updateByID", col, err)
	}
	return err
}

func DeleteByID(ctx context.Context, col *mongo.Collection, id string) error {
	_, err := col.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		logDBError("deleteByID", col, err)
	}
	return err
}

func logDBError(op string, col *mongo.Collection, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		log.Printf("[db] timeout during %s on %s: %v", op, col.Name(), err)
		return
	}
	log.Printf("[db] error during %s on %s: %v", op, col.Name(), err)
}
