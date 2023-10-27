package grpccoordinator

import (
	"context"

	"github.com/chroma/chroma-coordinator/internal/common"
	"github.com/chroma/chroma-coordinator/internal/model"
	"github.com/chroma/chroma-coordinator/internal/proto/coordinatorpb"
	"github.com/chroma/chroma-coordinator/internal/types"
	"github.com/pingcap/log"
	"go.uber.org/zap"
)

const errorCode = 500
const successCode = 200
const success = "ok"

func (s *Server) ResetState(context.Context, *coordinatorpb.ResetStateRequest) (*coordinatorpb.ResetStateResponse, error) {
	res := &coordinatorpb.ResetStateResponse{}
	err := s.coordinator.ResetState(context.Background())
	if err != nil {
		res.Status = failResponseWithError(err, errorCode)
		return res, err
	}
	res.Status = setResponseStatus(successCode)
	return res, nil
}

func (s *Server) CreateCollection(ctx context.Context, req *coordinatorpb.CreateCollectionRequest) (*coordinatorpb.CreateCollectionResponse, error) {
	getOrCreate := req.GetGetOrCreate()
	if getOrCreate {
		return s.getOrCreateCollection(ctx, req)
	} else {
		return s.createCollection(ctx, req)
	}
}

// Cases for get_or_create

// Case 0
// new_metadata is none, coll is an existing collection
// get_or_create should return the existing collection with existing metadata
// Essentially - an update with none is a no-op

// Case 1
// new_metadata is none, coll is a new collection
// get_or_create should create a new collection with the metadata of None

// Case 2
// new_metadata is not none, coll is an existing collection
// get_or_create should return the existing collection with updated metadata

// Case 3
// new_metadata is not none, coll is a new collection
// get_or_create should create a new collection with the new metadata, ignoring
// the metdata of in the input coll.

// The fact that we ignore the metadata of the generated collections is a
// bit weird, but it is the easiest way to excercise all cases
func (s *Server) getOrCreateCollection(ctx context.Context, req *coordinatorpb.CreateCollectionRequest) (*coordinatorpb.CreateCollectionResponse, error) {
	res := &coordinatorpb.CreateCollectionResponse{}
	name := req.GetName()
	collections, err := s.coordinator.GetCollections(ctx, types.NilUniqueID(), &name, nil)
	if err != nil {
		log.Error("error getting collections", zap.Error(err))
		res.Collection = &coordinatorpb.Collection{
			Id:        req.Id,
			Name:      req.Name,
			Dimension: req.Dimension,
			Metadata:  req.Metadata,
		}
		res.Created = false
		res.Status = failResponseWithError(err, errorCode)
		return res, nil
	}
	if len(collections) > 0 { // collection exists, need to update the metadata
		if req.Metadata != nil { // update existing collection with new metadata
			metadata, err := convertCollectionMetadataToModel(req.Metadata)
			if err != nil {
				log.Error("error converting collection metadata to model", zap.Error(err))
				res.Collection = &coordinatorpb.Collection{
					Id:        req.Id,
					Name:      req.Name,
					Dimension: req.Dimension,
					Metadata:  req.Metadata,
				}
				res.Created = false
				res.Status = failResponseWithError(err, errorCode)
				return res, nil
			}
			// update collection with new metadata
			updateCollection := &model.UpdateCollection{
				ID:       collections[0].ID,
				Metadata: metadata,
			}
			updatedCollection, err := s.coordinator.UpdateCollection(ctx, updateCollection)
			if err != nil {
				log.Error("error updating collection", zap.Error(err))
				res.Collection = &coordinatorpb.Collection{
					Id:        req.Id,
					Name:      req.Name,
					Dimension: req.Dimension,
					Metadata:  req.Metadata,
				}
				res.Created = false
				res.Status = failResponseWithError(err, errorCode)
				return res, nil
			}
			// sucessfully update the metadata
			res.Collection = convertCollectionToProto(updatedCollection)
			res.Created = false
			res.Status = setResponseStatus(successCode)
			return res, nil
		} else { // do nothing, return the existing collection
			res.Collection = &coordinatorpb.Collection{
				Id:        req.Id,
				Name:      req.Name,
				Dimension: req.Dimension,
			}
			res.Collection.Metadata = convertCollectionMetadataToProto(collections[0].Metadata)
			res.Created = false
			res.Status = setResponseStatus(successCode)
			return res, nil
		}
	} else { // collection does not exist, need to create it
		return s.createCollection(ctx, req)
	}
}

func (s *Server) createCollection(ctx context.Context, req *coordinatorpb.CreateCollectionRequest) (*coordinatorpb.CreateCollectionResponse, error) {
	res := &coordinatorpb.CreateCollectionResponse{}
	createCollection, err := convertToCreateCollectionModel(req)
	if err != nil {
		log.Error("error converting to create collection model", zap.Error(err))
		res.Collection = &coordinatorpb.Collection{
			Id:        req.Id,
			Name:      req.Name,
			Dimension: req.Dimension,
			Metadata:  req.Metadata,
		}
		res.Created = false
		res.Status = failResponseWithError(err, successCode)
		return res, nil
	}
	collection, err := s.coordinator.CreateCollection(ctx, createCollection)
	if err != nil {
		log.Error("error creating collection", zap.Error(err))
		res.Collection = &coordinatorpb.Collection{
			Id:        req.Id,
			Name:      req.Name,
			Dimension: req.Dimension,
			Metadata:  req.Metadata,
		}
		res.Created = false
		if err == common.ErrCollectionUniqueConstraintViolation {
			res.Status = failResponseWithError(err, 409)
		} else {
			res.Status = failResponseWithError(err, errorCode)
		}
		return res, nil
	}
	res.Collection = convertCollectionToProto(collection)
	res.Created = true
	res.Status = setResponseStatus(successCode)
	return res, nil
}

func (s *Server) GetCollections(ctx context.Context, req *coordinatorpb.GetCollectionsRequest) (*coordinatorpb.GetCollectionsResponse, error) {
	collectionID := req.Id
	collectionName := req.Name
	collectionTopic := req.Topic

	res := &coordinatorpb.GetCollectionsResponse{}

	parsedCollectionID, err := types.ToUniqueID(collectionID)
	if err != nil {
		log.Error("collection id format error", zap.String("collectionpd.id", *collectionID))
		res.Status = failResponseWithError(common.ErrCollectionIDFormat, errorCode)
		return res, nil
	}

	collections, err := s.coordinator.GetCollections(ctx, parsedCollectionID, collectionName, collectionTopic)
	if err != nil {
		log.Error("error getting collections", zap.Error(err))
		res.Status = failResponseWithError(err, errorCode)
		return res, nil
	}
	res.Collections = make([]*coordinatorpb.Collection, 0, len(collections))
	for _, collection := range collections {
		collectionpb := convertCollectionToProto(collection)
		res.Collections = append(res.Collections, collectionpb)
	}
	log.Info("collection service collections", zap.Any("collections", res.Collections))
	res.Status = setResponseStatus(successCode)
	return res, nil
}

func (s *Server) DeleteCollection(ctx context.Context, req *coordinatorpb.DeleteCollectionRequest) (*coordinatorpb.DeleteCollectionResponse, error) {
	collectionID := req.GetId()
	res := &coordinatorpb.DeleteCollectionResponse{}
	parsedCollectionID, err := types.Parse(collectionID)
	if err != nil {
		log.Error(err.Error(), zap.String("collectionpd.id", collectionID))
		res.Status = failResponseWithError(common.ErrCollectionIDFormat, errorCode)
		return res, nil
	}
	err = s.coordinator.DeleteCollection(ctx, parsedCollectionID)
	if err != nil {
		log.Error(err.Error(), zap.String("collectionpd.id", collectionID))
		if err == common.ErrCollectionDeleteNonExistingCollection {
			res.Status = failResponseWithError(err, 404)
		} else {
			res.Status = failResponseWithError(err, errorCode)
		}
		return res, nil
	}
	res.Status = setResponseStatus(successCode)
	return res, nil
}

func (s *Server) UpdateCollection(ctx context.Context, req *coordinatorpb.UpdateCollectionRequest) (*coordinatorpb.UpdateCollectionResponse, error) {
	res := &coordinatorpb.UpdateCollectionResponse{}

	collectionID := req.Id
	parsedCollectionID, err := types.ToUniqueID(&collectionID)
	if err != nil {
		log.Error("collection id format error", zap.String("collectionpd.id", collectionID))
		res.Status = failResponseWithError(common.ErrCollectionIDFormat, errorCode)
		return res, nil
	}

	updateCollection := &model.UpdateCollection{
		ID:        parsedCollectionID,
		Name:      req.Name,
		Topic:     req.Topic,
		Dimension: req.Dimension,
	}

	resetMetadata := req.GetResetMetadata()
	updateCollection.ResetMetadata = resetMetadata
	metadata := req.GetMetadata()
	// Case 1: if resetMetadata is true, then delete all metadata for the collection
	// Case 2: if resetMetadata is true and metadata is not nil -> THIS SHOULD NEVER HAPPEN
	// Case 3: if resetMetadata is false, and the metadata is not nil - set the metadata to the value in metadata
	// Case 4: if resetMetadata is false and metadata is nil, then leave the metadata as is
	if resetMetadata {
		if metadata != nil {
			log.Error("reset metadata is true and metadata is not nil", zap.Any("metadata", metadata))
			res.Status = failResponseWithError(common.ErrInvalidMetadataUpdate, errorCode)
			return res, nil
		} else {
			updateCollection.Metadata = nil
		}
	} else {
		if metadata != nil {
			modelMetadata, err := convertCollectionMetadataToModel(metadata)
			if err != nil {
				log.Error("error converting collection metadata to model", zap.Error(err))
				res.Status = failResponseWithError(err, errorCode)
				return res, nil
			}
			updateCollection.Metadata = modelMetadata
		} else {
			updateCollection.Metadata = nil
		}
	}

	_, err = s.coordinator.UpdateCollection(ctx, updateCollection)
	if err != nil {
		log.Error("error updating collection", zap.Error(err))
		res.Status = failResponseWithError(err, errorCode)
		return res, nil
	}

	res.Status = setResponseStatus(successCode)
	return res, nil
}

func failResponseWithError(err error, code int32) *coordinatorpb.Status {
	return &coordinatorpb.Status{
		Reason: err.Error(),
		Code:   code,
	}
}

func setResponseStatus(code int32) *coordinatorpb.Status {
	return &coordinatorpb.Status{
		Reason: success,
		Code:   code,
	}
}
