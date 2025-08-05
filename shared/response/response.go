package response

// SimpleMessage is a generic success message.
// swagger:model
type SimpleMessage struct {
	Message string `json:"message" example:"Result received"`
}

// ErrorMessage is a generic error message.
// swagger:model
type ErrorMessage struct {
	Error string `json:"error" example:"Failed to parse UUID"`
}

// ServerErrorMessage is a generic error payload.
// swagger:model
type ServerErrorMessage struct {
	Error string `json:"error" example:"Internal server error"`
}

// NotFoundMessage is a generic not found payload.
// swagger:model
type NotFoundMessage struct {
	Error string `json:"error" example:"resource not found"`
}
