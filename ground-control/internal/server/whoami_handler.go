package server

import (
	"net/http"
)

type whoamiResponse struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (s *Server) whoamiHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := GetUserFromContext(r.Context())
	if !ok {
		WriteJSONError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	resp := whoamiResponse{
		Username: user.Username,
		Role:     user.Role,
	}

	WriteJSONResponse(w, http.StatusOK, resp)
}
