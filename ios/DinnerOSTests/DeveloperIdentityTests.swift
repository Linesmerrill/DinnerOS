#if DEBUG

    import Foundation
    import Testing

    @testable import DinnerOS

    struct DeveloperIdentityTests {
        @Test func defaultSubjectKeepsTheOriginalIdentity() {
            let identity = DeveloperIdentity(subject: DeveloperIdentity.defaultSubject)

            #expect(identity.subject == "dev-simulator")
            #expect(identity.email == "dev-simulator@example.com")
            #expect(identity.displayName == "Dev Simulator")
        }

        @Test func eachSubjectGetsItsOwnEmailAndName() {
            let rachel = DeveloperIdentity(subject: "rachel-sim")
            let charlie = DeveloperIdentity(subject: "charlie-sim")

            #expect(rachel.email == "rachel-sim@example.com")
            #expect(rachel.displayName == "Rachel Sim")
            #expect(charlie.displayName == "Charlie Sim")
            #expect(rachel != charlie)
        }

        @Test(arguments: [" Rachel-Sim ", "RACHEL-SIM", "rachel-sim\n"])
        func subjectIsTrimmedAndLowercased(_ raw: String) {
            #expect(DeveloperIdentity(subject: raw) == DeveloperIdentity(subject: "rachel-sim"))
        }

        @Test(arguments: ["", "   ", "\n\t"])
        func blankSubjectFallsBackToTheDefault(_ raw: String) {
            #expect(DeveloperIdentity(subject: raw).subject == DeveloperIdentity.defaultSubject)
        }

        @Test func emailDropsCharactersAnAddressCantHold() {
            let identity = DeveloperIdentity(subject: "dev user+two")

            #expect(identity.subject == "dev user+two")
            #expect(identity.email == "dev-user-two@example.com")
            #expect(identity.displayName == "Dev User Two")
        }

        @Test func subjectOfPunctuationAloneStillYieldsAUsableEmail() {
            let identity = DeveloperIdentity(subject: "+++")

            #expect(identity.email == "dev-simulator@example.com")
            #expect(identity.displayName == "dev-simulator")
        }

        @Test func suggestionsLeadWithTheDefault() {
            #expect(DeveloperIdentity.suggestedSubjects.first == DeveloperIdentity.defaultSubject)
            #expect(Set(DeveloperIdentity.suggestedSubjects).count == DeveloperIdentity.suggestedSubjects.count)
        }
    }

    @MainActor
    struct DeveloperSignInModelTests {
        private func makeModel(
            handler: @escaping StubTransport.Handler
        ) throws -> (SignInModel, StubTransport) {
            let transport = StubTransport(handler)
            let api = AuthAPI(
                client: APIClient(baseURL: try #require(URL(string: Fixtures.baseURLString)), transport: transport))
            let session = AuthSession(api: api, store: InMemoryTokenStore())
            return (SignInModel(session: session, google: nil), transport)
        }

        @Test func developerSignInSendsTheChosenSubject() async throws {
            let (model, transport) = try makeModel { _ in
                (200, Fixtures.sessionJSON(access: "access-1", refresh: "refresh-1", displayName: "Rachel Sim"))
            }

            model.developerSubject = "  Rachel-Sim "
            await model.signInForDevelopment()

            #expect(model.errorMessage == nil)
            let body = try #require(transport.requests(to: "/api/v1/auth/dev").first?.jsonBody)
            #expect(body["subject"] == "rachel-sim")
            #expect(body["email"] == "rachel-sim@example.com")
            #expect(body["displayName"] == "Rachel Sim")
        }

        @Test func developerSignInDefaultsToTheSimulatorSubject() async throws {
            let (model, transport) = try makeModel { _ in
                (200, Fixtures.sessionJSON(access: "access-1", refresh: "refresh-1"))
            }

            await model.signInForDevelopment()

            let body = try #require(transport.requests(to: "/api/v1/auth/dev").first?.jsonBody)
            #expect(body["subject"] == "dev-simulator")
            #expect(body["displayName"] == "Dev Simulator")
        }

        @Test func retryResendsTheSubjectOnScreen() async throws {
            let failFirst = Counter()
            let (model, transport) = try makeModel { _ in
                failFirst.increment()
                guard failFirst.value > 1 else {
                    return (500, Fixtures.errorJSON(code: "internal_error"))
                }
                return (200, Fixtures.sessionJSON(access: "access-1", refresh: "refresh-1"))
            }

            model.developerSubject = "charlie-sim"
            await model.signInForDevelopment()
            #expect(model.canRetry)

            await model.retry()

            #expect(model.errorMessage == nil)
            let bodies = transport.requests(to: "/api/v1/auth/dev").compactMap(\.jsonBody)
            #expect(bodies.count == 2)
            #expect(bodies.allSatisfy { $0["subject"] == "charlie-sim" })
        }
    }

#endif
