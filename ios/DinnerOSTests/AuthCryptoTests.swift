import Foundation
import Testing

@testable import DinnerOS

struct PKCETests {
    /// RFC 7636, Appendix B.
    @Test func challengeMatchesRFC7636TestVector() {
        let pkce = PKCE(verifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
        #expect(pkce.challenge == "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM")
        #expect(PKCE.method == "S256")
    }

    @Test func generatedVerifiersAreUnreservedAndUnique() {
        let unreserved = CharacterSet(
            charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~")
        let verifiers = (0..<20).map { _ in PKCE.generate().verifier }

        for verifier in verifiers {
            #expect((43...128).contains(verifier.count))
            #expect(verifier.unicodeScalars.allSatisfy(unreserved.contains))
        }
        #expect(Set(verifiers).count == verifiers.count)
    }
}

struct NonceTests {
    @Test func sha256HexMatchesKnownVectors() {
        // FIPS 180-2 test vectors.
        #expect(Nonce.sha256Hex("abc") == "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
        #expect(Nonce.sha256Hex("") == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
    }

    @Test func generatedNoncesAreRandomURLSafeAndWithinAPILimit() {
        let nonces = (0..<20).map { _ in Nonce.generate() }
        for nonce in nonces {
            #expect(nonce.count == 43)
            #expect(nonce.count <= 512)
            #expect(!nonce.contains { "+/=".contains($0) })
            #expect(Nonce.sha256Hex(nonce).count == 64)
        }
        #expect(Set(nonces).count == nonces.count)
    }

    @Test func base64URLHasNoPadding() {
        #expect(Base64URL.encode(Data([0xfb, 0xff])) == "-_8")
    }
}

struct AppleSignInTests {
    @Test func formatsProvidedName() {
        var components = PersonNameComponents()
        components.givenName = "Ada"
        components.familyName = "Lovelace"
        let name = AppleSignIn.formattedFullName(components)
        #expect(name?.contains("Ada") == true)
        #expect(name?.contains("Lovelace") == true)
    }

    @Test func omitsMissingOrEmptyName() {
        #expect(AppleSignIn.formattedFullName(nil) == nil)
        #expect(AppleSignIn.formattedFullName(PersonNameComponents()) == nil)
    }

    @Test func capsNameAtAPILimit() {
        var components = PersonNameComponents()
        components.givenName = String(repeating: "a", count: 150)
        #expect(AppleSignIn.formattedFullName(components)?.count == 100)
    }
}
