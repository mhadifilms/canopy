import Foundation
import CryptoKit
import Testing

@testable import CanopyKit

@Suite("CoordinationClient Ed25519 Signing")
struct CoordinationClientSigningTests {

    /// Verify that CryptoKit Ed25519 produces 64-byte raw signatures
    /// compatible with Go's crypto/ed25519 (RFC 8032 pure Ed25519).
    @Test("Ed25519 signature is 64 bytes")
    func signatureLength() throws {
        let key = Curve25519.Signing.PrivateKey()
        let message = Data("test message".utf8)
        let signature = try key.signature(for: message)

        // Ed25519 signatures are always 64 bytes
        #expect(Data(signature).count == 64)
    }

    @Test("Ed25519 signature verifies with public key")
    func signatureVerification() throws {
        let key = Curve25519.Signing.PrivateKey()
        let message = Data("hello coordination server".utf8)
        let signature = try key.signature(for: message)

        // Verify with the corresponding public key
        let isValid = key.publicKey.isValidSignature(signature, for: message)
        #expect(isValid)
    }

    @Test("Ed25519 signature fails verification with wrong key")
    func signatureFailsWithWrongKey() throws {
        let key1 = Curve25519.Signing.PrivateKey()
        let key2 = Curve25519.Signing.PrivateKey()
        let message = Data("signed by key1".utf8)
        let signature = try key1.signature(for: message)

        // Should NOT verify with a different public key
        let isValid = key2.publicKey.isValidSignature(signature, for: message)
        #expect(!isValid)
    }

    @Test("Ed25519 signature fails verification with tampered message")
    func signatureFailsWithTamperedMessage() throws {
        let key = Curve25519.Signing.PrivateKey()
        let message = Data("original message".utf8)
        let tampered = Data("tampered message".utf8)
        let signature = try key.signature(for: message)

        let isValid = key.publicKey.isValidSignature(signature, for: tampered)
        #expect(!isValid)
    }

    @Test("Ed25519 public key is 32 bytes")
    func publicKeyLength() {
        let key = Curve25519.Signing.PrivateKey()
        #expect(key.publicKey.rawRepresentation.count == 32)
    }

    @Test("Ed25519 key round-trips through raw representation")
    func keyRoundTrip() throws {
        let original = Curve25519.Signing.PrivateKey()
        let raw = original.rawRepresentation

        let restored = try Curve25519.Signing.PrivateKey(rawRepresentation: raw)

        // Same public key
        #expect(original.publicKey.rawRepresentation == restored.publicKey.rawRepresentation)

        // Restored key can verify signatures from the original
        let message = Data("roundtrip test".utf8)
        let sig = try original.signature(for: message)
        let isValid = restored.publicKey.isValidSignature(sig, for: message)
        #expect(isValid)
    }

    @Test("Base64 encoding of signature is compatible with Go decoding")
    func base64SignatureEncoding() throws {
        let key = Curve25519.Signing.PrivateKey()
        let message = Data("check-in payload".utf8)
        let signature = try key.signature(for: message)
        let sigData = Data(signature)

        // Base64 encode (this is what we send to the coord server)
        let b64 = sigData.base64EncodedString()

        // Decode back
        guard let decoded = Data(base64Encoded: b64) else {
            Issue.record("Failed to decode base64 signature")
            return
        }

        #expect(decoded.count == 64)
        #expect(decoded == sigData)

        // Go's base64.StdEncoding uses standard alphabet, same as Foundation's default
        // Verify no URL-safe characters were used
        #expect(!b64.contains("-"))
        #expect(!b64.contains("_"))
    }

    @Test("Base64 encoding of public key is compatible with Go decoding")
    func base64PublicKeyEncoding() throws {
        let key = Curve25519.Signing.PrivateKey()
        let pubB64 = key.publicKey.rawRepresentation.base64EncodedString()

        // Decode back
        guard let decoded = Data(base64Encoded: pubB64) else {
            Issue.record("Failed to decode base64 public key")
            return
        }

        #expect(decoded.count == 32)
        #expect(decoded == key.publicKey.rawRepresentation)

        // Verify round-trip through CryptoKit
        let restoredPub = try Curve25519.Signing.PublicKey(rawRepresentation: decoded)
        #expect(restoredPub.rawRepresentation == key.publicKey.rawRepresentation)
    }

    @Test("Signing JSON payload matches Go verification flow")
    func signJSONPayload() throws {
        let key = Curve25519.Signing.PrivateKey()

        // Simulate the check-in request body (sorted keys for determinism)
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys

        struct TestPayload: Codable {
            let deviceKey: String
            let timestamp: String
            let wgPublicKey: String
        }

        let payload = TestPayload(
            deviceKey: key.publicKey.rawRepresentation.base64EncodedString(),
            timestamp: "2026-02-22T10:00:00Z",
            wgPublicKey: "dGVzdF93Z19rZXk="
        )

        let data = try encoder.encode(payload)
        let signature = try key.signature(for: data)

        // Go server would:
        // 1. Decode base64 public key from deviceKey field
        // 2. Decode base64 signature from sig field
        // 3. Verify signature over the JSON body bytes
        let isValid = key.publicKey.isValidSignature(signature, for: data)
        #expect(isValid)

        // Verify that re-encoding produces the same bytes (sortedKeys ensures this)
        let reEncoded = try encoder.encode(payload)
        #expect(data == reEncoded)

        // And the signature still verifies against re-encoded data
        let stillValid = key.publicKey.isValidSignature(signature, for: reEncoded)
        #expect(stillValid)
    }

    @Test("Signed bearer token format is parseable")
    func signedBearerTokenFormat() throws {
        let key = Curve25519.Signing.PrivateKey()
        let timestamp = "2026-02-22T10:00:00Z"
        let timestampData = Data(timestamp.utf8)
        let signature = try key.signature(for: timestampData)

        let pubB64 = key.publicKey.rawRepresentation.base64EncodedString()
        let sigB64 = Data(signature).base64EncodedString()
        // Token format uses "." separator to avoid conflict with ":" in timestamps
        let token = "\(pubB64).\(timestamp).\(sigB64)"

        // Parse the token (as the coord server would)
        let parts = token.split(separator: ".", maxSplits: 2)
        #expect(parts.count == 3)

        let parsedPubB64 = String(parts[0])
        let parsedTimestamp = String(parts[1])
        let parsedSigB64 = String(parts[2])

        // Reconstruct and verify
        guard let pubData = Data(base64Encoded: parsedPubB64),
              let sigData = Data(base64Encoded: parsedSigB64) else {
            Issue.record("Failed to decode base64 components")
            return
        }

        let publicKey = try Curve25519.Signing.PublicKey(rawRepresentation: pubData)
        let parsedTimestampData = Data(parsedTimestamp.utf8)

        let isValid = publicKey.isValidSignature(sigData, for: parsedTimestampData)
        #expect(isValid)
    }
}

@Suite("PairingQRPayload Parsing")
struct PairingQRPayloadTests {

    @Test("Parse valid QR JSON")
    func parseValid() {
        let json = """
        {
            "code": "482917",
            "wg_pub": "dGVzdF93Z19wdWJsaWNfa2V5X2Jhc2U2NA==",
            "identity": "dGVzdF9pZGVudGl0eV9wdWJsaWNfa2V5",
            "coord": "coord.canopy.dev",
            "endpoints": ["192.168.1.100:51820", "203.0.113.42:51820"]
        }
        """

        let payload = parsePairingQRPayload(json)
        #expect(payload != nil)
        #expect(payload?.code == "482917")
        #expect(payload?.wgPub == "dGVzdF93Z19wdWJsaWNfa2V5X2Jhc2U2NA==")
        #expect(payload?.identity == "dGVzdF9pZGVudGl0eV9wdWJsaWNfa2V5")
        #expect(payload?.coord == "coord.canopy.dev")
        #expect(payload?.endpoints.count == 2)
    }

    @Test("Parse invalid JSON returns nil")
    func parseInvalid() {
        #expect(parsePairingQRPayload("not json") == nil)
        #expect(parsePairingQRPayload("{}") == nil)
        #expect(parsePairingQRPayload("") == nil)
    }

    @Test("Parse JSON with missing fields returns nil")
    func parseMissingFields() {
        let json = """
        {"code": "123456", "wg_pub": "key"}
        """
        #expect(parsePairingQRPayload(json) == nil)
    }
}
