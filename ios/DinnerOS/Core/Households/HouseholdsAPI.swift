import Foundation

/// Typed wrappers for the household and invitation endpoints (`/api/v1/households/*`,
/// `/api/v1/invitations/accept`). Every call needs an access token; use them through
/// `AuthSession.authorized`.
nonisolated struct HouseholdsAPI: Sendable {
    let client: APIClient

    func createHousehold(name: String, timeZone: String, defaultServings: Int?, accessToken: String)
        async throws -> CreateHouseholdResponse
    {
        let body = CreateHouseholdBody(name: name, timeZone: timeZone, defaultServings: defaultServings)
        return try await client.send(
            try APIRequest.post("/api/v1/households", body: body).authorized(with: accessToken))
    }

    func listHouseholds(accessToken: String) async throws -> [HouseholdListItem] {
        let response: HouseholdListResponse = try await client.send(
            APIRequest.get("/api/v1/households").authorized(with: accessToken))
        return response.items
    }

    func household(id: String, accessToken: String) async throws -> HouseholdDetail {
        try await client.send(APIRequest.get("/api/v1/households/\(id)").authorized(with: accessToken))
    }

    func updateHousehold(id: String, changes: HouseholdChanges, accessToken: String) async throws -> Household {
        try await client.send(
            try APIRequest.patch("/api/v1/households/\(id)", body: changes).authorized(with: accessToken))
    }

    func changeRole(householdID: String, userID: String, to role: HouseholdRole, accessToken: String)
        async throws -> HouseholdMember
    {
        try await client.send(
            try APIRequest.patch(
                "/api/v1/households/\(householdID)/members/\(userID)", body: ChangeRoleBody(role: role)
            ).authorized(with: accessToken))
    }

    /// Removes a member. Pass your own user ID to leave the household.
    func removeMember(householdID: String, userID: String, accessToken: String) async throws {
        try await client.sendIgnoringBody(
            APIRequest.delete("/api/v1/households/\(householdID)/members/\(userID)").authorized(with: accessToken))
    }

    func createInvitation(householdID: String, email: String, role: HouseholdRole, accessToken: String)
        async throws -> CreateInvitationResponse
    {
        try await client.send(
            try APIRequest.post(
                "/api/v1/households/\(householdID)/invitations", body: CreateInvitationBody(email: email, role: role)
            ).authorized(with: accessToken))
    }

    /// Pending invitations only.
    func pendingInvitations(householdID: String, accessToken: String) async throws -> [HouseholdInvitation] {
        let response: InvitationListResponse = try await client.send(
            APIRequest.get("/api/v1/households/\(householdID)/invitations").authorized(with: accessToken))
        return response.items
    }

    func revokeInvitation(householdID: String, invitationID: String, accessToken: String) async throws {
        try await client.sendIgnoringBody(
            APIRequest.delete("/api/v1/households/\(householdID)/invitations/\(invitationID)")
                .authorized(with: accessToken))
    }

    /// The token or code travels in the JSON body, never in the URL.
    func acceptInvitation(_ secret: InvitationSecret, accessToken: String) async throws -> AcceptInvitationResponse {
        let body =
            switch secret {
            case .token(let token): AcceptInvitationBody(token: token, code: nil)
            case .code(let code): AcceptInvitationBody(token: nil, code: code)
            }
        return try await client.send(
            try APIRequest.post("/api/v1/invitations/accept", body: body).authorized(with: accessToken))
    }
}

// Request bodies. The API rejects unknown fields; nil optionals are omitted.

private nonisolated struct CreateHouseholdBody: Encodable {
    let name: String
    let timeZone: String
    let defaultServings: Int?
}

private nonisolated struct ChangeRoleBody: Encodable {
    let role: HouseholdRole
}

private nonisolated struct CreateInvitationBody: Encodable {
    let email: String
    let role: HouseholdRole
}

private nonisolated struct AcceptInvitationBody: Encodable {
    let token: String?
    let code: String?
}
