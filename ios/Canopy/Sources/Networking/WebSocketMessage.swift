import Foundation

/// Messages sent from the iOS client to the daemon (section 3.8.1).
enum ClientMessage: Codable, Sendable {

    case listSessions(ListSessions)
    case subscribe(Subscribe)
    case unsubscribe(Unsubscribe)
    case getHistory(GetHistory)
    case input(Input)
    case inputRaw(InputRaw)
    case signal(Signal)
    case readFile(ReadFile)
    case searchSessions(SearchSessions)
    case ping
    case getInfo

    // MARK: - Payload types

    struct ListSessions: Codable, Sendable {
        var filter: Filter?
        var limit: Int?
        var offset: Int?

        struct Filter: Codable, Sendable {
            var status: [SessionStatus]?
            var includeEnded: Bool?
            var since: Date?

            enum CodingKeys: String, CodingKey {
                case status
                case includeEnded = "include_ended"
                case since
            }
        }
    }

    struct Subscribe: Codable, Sendable {
        let sessionId: String

        enum CodingKeys: String, CodingKey {
            case sessionId = "session_id"
        }
    }

    struct Unsubscribe: Codable, Sendable {
        let sessionId: String

        enum CodingKeys: String, CodingKey {
            case sessionId = "session_id"
        }
    }

    struct GetHistory: Codable, Sendable {
        let sessionId: String
        var since: Date?
        var limit: Int?

        enum CodingKeys: String, CodingKey {
            case sessionId = "session_id"
            case since, limit
        }
    }

    struct Input: Codable, Sendable {
        let sessionId: String
        let text: String

        enum CodingKeys: String, CodingKey {
            case sessionId = "session_id"
            case text
        }
    }

    struct InputRaw: Codable, Sendable {
        let sessionId: String
        let bytesB64: String

        enum CodingKeys: String, CodingKey {
            case sessionId = "session_id"
            case bytesB64 = "bytes_b64"
        }
    }

    struct Signal: Codable, Sendable {
        let sessionId: String
        let signal: String

        enum CodingKeys: String, CodingKey {
            case sessionId = "session_id"
            case signal
        }
    }

    struct ReadFile: Codable, Sendable {
        let path: String
        var maxBytes: Int?

        enum CodingKeys: String, CodingKey {
            case path
            case maxBytes = "max_bytes"
        }
    }

    struct SearchSessions: Codable, Sendable {
        let query: String
        var dateRange: DateRange?
        var limit: Int?

        struct DateRange: Codable, Sendable {
            let from: Date
            let to: Date
        }

        enum CodingKeys: String, CodingKey {
            case query
            case dateRange = "date_range"
            case limit
        }
    }

    // MARK: - Codable

    private enum TypeKey: String, Codable {
        case listSessions = "list_sessions"
        case subscribe
        case unsubscribe
        case getHistory = "get_history"
        case input
        case inputRaw = "input_raw"
        case signal
        case readFile = "read_file"
        case searchSessions = "search_sessions"
        case ping
        case getInfo = "get_info"
    }

    private enum CodingKeys: String, CodingKey {
        case type
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let type = try container.decode(TypeKey.self, forKey: .type)

        switch type {
        case .listSessions:    self = .listSessions(try ListSessions(from: decoder))
        case .subscribe:       self = .subscribe(try Subscribe(from: decoder))
        case .unsubscribe:     self = .unsubscribe(try Unsubscribe(from: decoder))
        case .getHistory:      self = .getHistory(try GetHistory(from: decoder))
        case .input:           self = .input(try Input(from: decoder))
        case .inputRaw:        self = .inputRaw(try InputRaw(from: decoder))
        case .signal:          self = .signal(try Signal(from: decoder))
        case .readFile:        self = .readFile(try ReadFile(from: decoder))
        case .searchSessions:  self = .searchSessions(try SearchSessions(from: decoder))
        case .ping:            self = .ping
        case .getInfo:         self = .getInfo
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)

        switch self {
        case .listSessions(let v):
            try container.encode(TypeKey.listSessions, forKey: .type)
            try v.encode(to: encoder)
        case .subscribe(let v):
            try container.encode(TypeKey.subscribe, forKey: .type)
            try v.encode(to: encoder)
        case .unsubscribe(let v):
            try container.encode(TypeKey.unsubscribe, forKey: .type)
            try v.encode(to: encoder)
        case .getHistory(let v):
            try container.encode(TypeKey.getHistory, forKey: .type)
            try v.encode(to: encoder)
        case .input(let v):
            try container.encode(TypeKey.input, forKey: .type)
            try v.encode(to: encoder)
        case .inputRaw(let v):
            try container.encode(TypeKey.inputRaw, forKey: .type)
            try v.encode(to: encoder)
        case .signal(let v):
            try container.encode(TypeKey.signal, forKey: .type)
            try v.encode(to: encoder)
        case .readFile(let v):
            try container.encode(TypeKey.readFile, forKey: .type)
            try v.encode(to: encoder)
        case .searchSessions(let v):
            try container.encode(TypeKey.searchSessions, forKey: .type)
            try v.encode(to: encoder)
        case .ping:
            try container.encode(TypeKey.ping, forKey: .type)
        case .getInfo:
            try container.encode(TypeKey.getInfo, forKey: .type)
        }
    }
}

// MARK: - Daemon → Client Messages (section 3.8.2)

/// Messages received from the daemon over WebSocket.
enum DaemonMessage: Sendable, Hashable {

    case sessionList(SessionListPayload)
    case event(EventPayload)
    case sessionStatus(SessionStatusPayload)
    case sessionStarted(SessionStartedPayload)
    case sessionEnded(SessionEndedPayload)
    case history(HistoryPayload)
    case fileContents(FileContentsPayload)
    case searchResults(SearchResultsPayload)
    case info(InfoPayload)
    case error(ErrorPayload)
    case pong
    case syncLost(SyncLostPayload)

    // MARK: - Payload types

    struct SessionListPayload: Codable, Sendable, Hashable {
        let sessions: [Session]
        let total: Int
    }

    struct EventPayload: Codable, Sendable, Hashable {
        let sessionId: String
        let event: SessionEvent

        enum CodingKeys: String, CodingKey {
            case sessionId = "session_id"
            case event
        }
    }

    struct SessionStatusPayload: Codable, Sendable, Hashable {
        let sessionId: String
        let status: SessionStatus
        let previousStatus: SessionStatus
        let detail: String?

        enum CodingKeys: String, CodingKey {
            case sessionId = "session_id"
            case status
            case previousStatus = "previous_status"
            case detail
        }
    }

    struct SessionStartedPayload: Codable, Sendable, Hashable {
        let session: Session
    }

    struct SessionEndedPayload: Codable, Sendable, Hashable {
        let sessionId: String
        let endedAt: Date
        let lastExitCode: Int?

        enum CodingKeys: String, CodingKey {
            case sessionId = "session_id"
            case endedAt = "ended_at"
            case lastExitCode = "last_exit_code"
        }
    }

    struct HistoryPayload: Codable, Sendable, Hashable {
        let sessionId: String
        let events: [SessionEvent]
        let hasMore: Bool
        let nextCursor: String?

        enum CodingKeys: String, CodingKey {
            case sessionId = "session_id"
            case events
            case hasMore = "has_more"
            case nextCursor = "next_cursor"
        }
    }

    struct FileContentsPayload: Codable, Sendable, Hashable {
        let path: String
        let content: String
        let language: String?
        let sizeBytes: Int

        enum CodingKeys: String, CodingKey {
            case path, content, language
            case sizeBytes = "size_bytes"
        }
    }

    struct SearchResultsPayload: Codable, Sendable, Hashable {
        let query: String
        let results: [SearchResult]

        struct SearchResult: Codable, Sendable, Hashable {
            let sessionId: String
            let title: String?
            let startedAt: Date
            let matches: [Match]

            struct Match: Codable, Sendable, Hashable {
                let eventType: String
                let ts: Date
                let snippet: String

                enum CodingKeys: String, CodingKey {
                    case eventType = "event_type"
                    case ts, snippet
                }
            }

            enum CodingKeys: String, CodingKey {
                case sessionId = "session_id"
                case title
                case startedAt = "started_at"
                case matches
            }
        }
    }

    struct InfoPayload: Codable, Sendable, Hashable {
        let hostname: String
        let deviceId: String
        let version: String
        let activeSessions: Int

        enum CodingKeys: String, CodingKey {
            case hostname
            case deviceId = "device_id"
            case version
            case activeSessions = "active_sessions"
        }
    }

    struct ErrorPayload: Codable, Sendable, Hashable {
        let code: String
        let message: String
    }

    struct SyncLostPayload: Codable, Sendable, Hashable {
        let sessionId: String
        let resumeFrom: String

        enum CodingKeys: String, CodingKey {
            case sessionId = "session_id"
            case resumeFrom = "resume_from"
        }
    }
}

// MARK: - DaemonMessage Codable

extension DaemonMessage: Codable {

    private enum TypeKey: String, Codable {
        case sessionList = "session_list"
        case event
        case sessionStatus = "session_status"
        case sessionStarted = "session_started"
        case sessionEnded = "session_ended"
        case history
        case fileContents = "file_contents"
        case searchResults = "search_results"
        case info
        case error
        case pong
        case syncLost = "sync_lost"
    }

    private enum CodingKeys: String, CodingKey {
        case type
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let type = try container.decode(TypeKey.self, forKey: .type)

        switch type {
        case .sessionList:    self = .sessionList(try SessionListPayload(from: decoder))
        case .event:          self = .event(try EventPayload(from: decoder))
        case .sessionStatus:  self = .sessionStatus(try SessionStatusPayload(from: decoder))
        case .sessionStarted: self = .sessionStarted(try SessionStartedPayload(from: decoder))
        case .sessionEnded:   self = .sessionEnded(try SessionEndedPayload(from: decoder))
        case .history:        self = .history(try HistoryPayload(from: decoder))
        case .fileContents:   self = .fileContents(try FileContentsPayload(from: decoder))
        case .searchResults:  self = .searchResults(try SearchResultsPayload(from: decoder))
        case .info:           self = .info(try InfoPayload(from: decoder))
        case .error:          self = .error(try ErrorPayload(from: decoder))
        case .pong:           self = .pong
        case .syncLost:       self = .syncLost(try SyncLostPayload(from: decoder))
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)

        switch self {
        case .sessionList(let v):
            try container.encode(TypeKey.sessionList, forKey: .type)
            try v.encode(to: encoder)
        case .event(let v):
            try container.encode(TypeKey.event, forKey: .type)
            try v.encode(to: encoder)
        case .sessionStatus(let v):
            try container.encode(TypeKey.sessionStatus, forKey: .type)
            try v.encode(to: encoder)
        case .sessionStarted(let v):
            try container.encode(TypeKey.sessionStarted, forKey: .type)
            try v.encode(to: encoder)
        case .sessionEnded(let v):
            try container.encode(TypeKey.sessionEnded, forKey: .type)
            try v.encode(to: encoder)
        case .history(let v):
            try container.encode(TypeKey.history, forKey: .type)
            try v.encode(to: encoder)
        case .fileContents(let v):
            try container.encode(TypeKey.fileContents, forKey: .type)
            try v.encode(to: encoder)
        case .searchResults(let v):
            try container.encode(TypeKey.searchResults, forKey: .type)
            try v.encode(to: encoder)
        case .info(let v):
            try container.encode(TypeKey.info, forKey: .type)
            try v.encode(to: encoder)
        case .error(let v):
            try container.encode(TypeKey.error, forKey: .type)
            try v.encode(to: encoder)
        case .pong:
            try container.encode(TypeKey.pong, forKey: .type)
        case .syncLost(let v):
            try container.encode(TypeKey.syncLost, forKey: .type)
            try v.encode(to: encoder)
        }
    }
}
