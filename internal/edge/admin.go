// Administration surface for the Edge, served over the owner-only IPC socket
// (agentagotchi.admin.v1). The Edge CLI, the optional Native SDK app, and
// future clients are thin clients over this surface; authorization lives here
// (the socket itself is the owner-only boundary).
package edge

import (
	"encoding/json"
	"net"
	"time"

	"agentagotchi.local/agentagotchi/internal/contract"
	"agentagotchi.local/agentagotchi/internal/pairing"
)

type adminRequest struct {
	Schema       string `json:"schema"`
	Type         string `json:"type"`
	CodeID       string `json:"codeId,omitempty"`
	CredentialID string `json:"credentialId,omitempty"`
	Role         string `json:"role,omitempty"`
	ClientName   string `json:"clientName,omitempty"`
}

type adminResponse struct {
	Schema      string                `json:"schema"`
	Type        string                `json:"type"`
	OK          bool                  `json:"ok"`
	Error       string                `json:"error,omitempty"`
	Status      *contract.AdminStatus `json:"status,omitempty"`
	Pending     []pairing.Code        `json:"pending,omitempty"`
	Credentials []pairing.Credential  `json:"credentials,omitempty"`
	Code        *pairing.Code         `json:"code,omitempty"`
}

func adminReply(conn net.Conn, response adminResponse) {
	response.Schema = contract.SchemaAdminV1
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	b, err := json.Marshal(response)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(b, '\n'))
}

func (s *Service) handleAdmin(conn net.Conn, frame []byte) {
	var request adminRequest
	if err := json.Unmarshal(frame, &request); err != nil || request.Schema != contract.SchemaAdminV1 {
		return
	}
	reply := adminResponse{Type: request.Type, OK: true}
	switch request.Type {
	case "status":
		reply.Status = s.adminStatus()
	case "pairing_begin":
		code, err := s.ceremony.RequestCode(pairing.Role(request.Role), request.ClientName)
		if err != nil {
			reply.OK, reply.Error = false, err.Error()
			break
		}
		reply.Code = &code
	case "pairing_pending":
		reply.Pending = s.ceremony.Pending()
	case "pairing_approve":
		if err := s.ceremony.Approve(request.CodeID); err != nil {
			reply.OK, reply.Error = false, err.Error()
		}
	case "pairing_deny":
		if err := s.ceremony.Deny(request.CodeID); err != nil {
			reply.OK, reply.Error = false, err.Error()
		}
	case "pairing_list":
		reply.Credentials = s.ceremony.List()
	case "pairing_revoke":
		if err := s.RevokePairing(request.CredentialID); err != nil {
			reply.OK, reply.Error = false, err.Error()
		}
	case "pairing_redeem":
		adminReply(conn, adminResponse{
			Type: request.Type, OK: false,
			Error: "redeem happens on the connecting client, not the admin surface",
		})
		return
	default:
		reply.OK, reply.Error = false, "unknown admin request type"
	}
	adminReply(conn, reply)
}

func (s *Service) adminStatus() *contract.AdminStatus {
	snap := s.core.Snapshot(s.edgeID, "edge")
	return &contract.AdminStatus{
		Schema: contract.SchemaAdminV1, Type: "status", Role: "edge",
		Version: s.version, StartedAt: s.startedAt,
		PairedDevices:  len(s.ceremony.List()),
		ConnectedPeers: len(s.hub.snapshot()),
		TaskPresences:  len(snap.Tasks), AggregateState: snap.AggregateState,
	}
}
