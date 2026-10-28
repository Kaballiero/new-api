package shared

import "github.com/QuantumNous/new-api/internal/adapter/admission"

// Orders is the single per-order admission registry injected into both callback families.
var Orders admission.OrderAdmission
