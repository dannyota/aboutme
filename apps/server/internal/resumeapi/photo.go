package resumeapi

import "net/http"

func photoRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPost, Pattern: apiResumePath + "/{id}/photo", Operation: "uploadResumePhoto", Mutation: true, Upload: true, Stub: true, OperationKind: operationPhotoCandidate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUploadResumePhoto},
		{Method: http.MethodGet, Pattern: apiResumePath + "/{id}/photo", Operation: "getResumePhoto", Stub: true, Handler: (*Service).handleGetResumePhoto},
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}/photo", Operation: "updateResumePhotoCrop", Mutation: true, Stub: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpdateResumePhotoCrop},
		{Method: http.MethodDelete, Pattern: apiResumePath + "/{id}/photo", Operation: "deleteResumePhoto", Mutation: true, Stub: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleDeleteResumePhoto},
	}
}

func (s *Service) handleUploadResumePhoto(w http.ResponseWriter, _ *http.Request) {
	writeConstructionStub(w)
}
func (s *Service) handleGetResumePhoto(w http.ResponseWriter, _ *http.Request) {
	writeConstructionStub(w)
}
func (s *Service) handleUpdateResumePhotoCrop(w http.ResponseWriter, _ *http.Request) {
	writeConstructionStub(w)
}
func (s *Service) handleDeleteResumePhoto(w http.ResponseWriter, _ *http.Request) {
	writeConstructionStub(w)
}
