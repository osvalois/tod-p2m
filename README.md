# tod-p2m: Torrent-On-Demand Streaming Platform

[![Go Report Card](https://goreportcard.com/badge/github.com/osvalois/tod-p2m)](https://goreportcard.com/report/github.com/osvalois/tod-p2m)
[![GoDoc](https://godoc.org/github.com/osvalois/tod-p2m?status.svg)](https://godoc.org/github.com/osvalois/tod-p2m)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Build Status](https://github.com/osvalois/tod-p2m/workflows/CI/badge.svg)](https://github.com/osvalois/tod-p2m/actions)
[![Coverage Status](https://coveralls.io/repos/github/osvalois/tod-p2m/badge.svg?branch=main)](https://coveralls.io/github/osvalois/tod-p2m?branch=main)
[![Docker Pulls](https://img.shields.io/docker/pulls/osvalois/tod-p2m.svg)](https://hub.docker.com/r/osvalois/tod-p2m)

## Table of Contents

1. [Introduction](#introduction)
2. [Key Features](#key-features)
3. [System Architecture](#system-architecture)
4. [Installation and Setup](#installation-and-setup)
5. [Configuration](#configuration)
6. [API Reference](#api-reference)
7. [Usage Examples](#usage-examples)
8. [Performance Optimization](#performance-optimization)
9. [Security Considerations](#security-considerations)
10. [Monitoring and Logging](#monitoring-and-logging)
11. [Testing Strategy](#testing-strategy)
12. [Deployment](#deployment)
13. [Contributing](#contributing)
14. [Versioning](#versioning)
15. [License](#license)
16. [Acknowledgments](#acknowledgments)
17. [Support and Contact](#support-and-contact)

## 1. Introduction

tod-p2m (Torrent-On-Demand Streaming Platform) is a high-performance, scalable application designed for streaming media content directly from torrent files. Built with Go, it leverages the power of BitTorrent protocol to provide seamless, on-demand access to a wide range of media formats including video, audio, images, and documents.

### 1.1 Purpose

The primary goal of tod-p2m is to offer a robust, efficient, and user-friendly solution for streaming torrent-based content, eliminating the need for complete downloads before playback. This platform is ideal for applications requiring quick access to large media files, such as video-on-demand services, digital libraries, or content distribution networks.

### 1.2 Target Audience

- Software developers building media streaming applications
- System administrators managing content delivery networks
- Researchers working on peer-to-peer technologies
- Media companies looking for efficient content distribution solutions

## 2. Key Features

- **Magnet Link Support**: Seamless integration with magnet links for instant content access.
- **Multi-Protocol Streaming**: 
  - HTTP Live Streaming (HLS) for adaptive bitrate streaming
  - HTTP byte-range requests for progressive downloading
- **Format Versatility**: Support for various media formats including video (MP4, WebM), audio (MP3, OGG), images (JPEG, PNG), and documents (PDF, TXT).
- **Dynamic Rate Limiting**: Configurable bandwidth management for both download and upload streams.
- **Intelligent Caching**: Sophisticated caching mechanisms to optimize resource usage and enhance performance.
- **Cross-Origin Resource Sharing (CORS)**: Built-in CORS support for seamless integration with web applications.
- **Concurrent Connection Handling**: Efficient management of multiple simultaneous connections leveraging Go's concurrency model.
- **Real-time Analytics**: Detailed metrics and analytics for monitoring system performance and user engagement.

## 3. System Architecture

tod-p2m is built on a modular, microservices-oriented architecture, ensuring scalability, maintainability, and ease of deployment.

### 3.1 High-Level Architecture Diagram

```
┌─────────────────┐   ┌─────────────────┐   ┌─────────────────┐
│    API Gateway  │   │  Torrent Engine │   │  Media Streamer │
│                 │   │                 │   │                 │
│  ┌───────────┐  │   │  ┌───────────┐  │   │  ┌───────────┐  │
│  │ Rate      │  │   │  │ Torrent   │  │   │  │ HLS       │  │
│  │ Limiter   │◄─┼───┼─▶│ Manager   │◄─┼───┼─▶│ Generator │  │
│  └───────────┘  │   │  └───────────┘  │   │  └───────────┘  │
│                 │   │                 │   │                 │
│  ┌───────────┐  │   │  ┌───────────┐  │   │  ┌───────────┐  │
│  │ Auth      │  │   │  │ Piece     │  │   │  │ MIME      │  │
│  │ Middleware│  │   │  │ Selector  │  │   │  │ Handler   │  │
│  └───────────┘  │   │  └───────────┘  │   │  └───────────┘  │
└─────────────────┘   └─────────────────┘   └─────────────────┘
         ▲                     ▲                     ▲
         │                     │                     │
         └─────────────────────┼─────────────────────┘
                               │
                   ┌───────────────────────┐
                   │      Storage&DB       │
                   └───────────────────────┘
```

### 3.2 Component Descriptions

- **API Gateway**: Handles incoming requests, authentication, and rate limiting.
- **Torrent Engine**: Manages torrent downloads, piece selection, and peer communication.
- **Media Streamer**: Responsible for adaptive streaming and format-specific handling.
- **Database**: Stores metadata and caching data.

### 3.3 Technology Stack

- **Backend**: Go 1.18+
- **Frameworks**: 
  - HTTP Router: [Go-Chi](https://github.com/go-chi/chi)
  - Torrent Client: [anacrolix/torrent](https://github.com/anacrolix/torrent)
- **Logging**: [zerolog](https://github.com/rs/zerolog)
- **Database**: Redis for caching, PostgreSQL for persistent storage
- **Containerization**: Docker
- **Orchestration**: Kubernetes (optional for large-scale deployments)

## 4. Installation and Setup

### 4.1 Prerequisites

- Go 1.18 or higher
- Docker and Docker Compose (for containerized deployment)
- Git

### 4.2 Local Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/osvalois/tod-p2m.git
   cd tod-p2m
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Build the application:
   ```bash
   go build -o tod-p2m ./cmd/server
   ```

4. Run the application:
   ```bash
   ./tod-p2m
   ```

### 4.3 Docker Installation

1. Build the Docker image:
   ```bash
   docker build -t tod-p2m:latest .
   ```

2. Run the container:
   ```bash
   docker run -p 8080:8080 -v /path/to/config:/app/config tod-p2m:latest
   ```

## 5. Configuration

tod-p2m uses a YAML configuration file and environment variables for flexible setup.

### 5.1 Configuration File (config.yaml)

```yaml
server:
  port: 8080
  host: "0.0.0.0"

logging:
  level: "info"
  format: "json"

torrent:
  max_connections: 100
  download_rate_limit: 0  # 0 means unlimited
  upload_rate_limit: 0
  cache_size: 100  # Number of torrents to keep in cache
  cleanup_interval: "10m"

streaming:
  hls_segment_duration: 10
  buffer_size: 1048576  # 1MB

security:
  enable_cors: true
  allowed_origins: ["*"]

database:
  type: "postgres"
  dsn: "postgres://user:password@localhost/tod_p2m?sslmode=disable"

redis:
  address: "localhost:6379"
  password: ""
  db: 0
```

### 5.2 Environment Variables

Environment variables override config file settings. Use the prefix `TOD_P2M_` for all variables.

Example:
```bash
export TOD_P2M_SERVER_PORT=9090
export TOD_P2M_LOGGING_LEVEL=debug
```

## 6. API Reference

Detailed API documentation is available in the [API Reference](docs/API.md) document. Here's a summary of the main endpoints:

- `GET /torrent/{infoHash}`: Retrieve torrent metadata
- `GET /stream/{infoHash}/{fileID}`: Stream a file via HTTP
- `GET /hls/{infoHash}/{fileID}/playlist.m3u8`: Get HLS playlist
- `GET /hls/{infoHash}/{fileID}/{segmentID}.ts`: Get HLS segment
- `GET /document/{infoHash}/{fileID}`: Stream a document
- `GET /image/{infoHash}/{fileID}`: Stream an image

## 7. Usage Examples

### 7.1 Streaming a Video

```javascript
const videoPlayer = document.getElementById('video-player');
const infoHash = '1234567890abcdef1234567890abcdef12345678';
const fileID = 0;

videoPlayer.src = `http://localhost:8080/hls/${infoHash}/${fileID}/playlist.m3u8`;
videoPlayer.play();
```

### 7.2 Fetching Torrent Metadata

```bash
curl http://localhost:8080/torrent/1234567890abcdef1234567890abcdef12345678
```

Response:
```json
{
  "infoHash": "1234567890abcdef1234567890abcdef12345678",
  "name": "Big Buck Bunny",
  "files": [
    {
      "id": 0,
      "name": "big_buck_bunny.mp4",
      "size": 276445467,
      "progress": 0.15
    }
  ]
}
```

## 8. Performance Optimization

tod-p2m implements several strategies to ensure high performance and efficient resource utilization:

### 8.1 Caching

- In-memory caching of frequently accessed torrent metadata
- Disk caching of popular content pieces to reduce network overhead

### 8.2 Connection Pooling

- Reuse of BitTorrent peer connections to minimize handshake overhead
- Database connection pooling for efficient resource management

### 8.3 Asynchronous Processing

- Non-blocking I/O operations leveraging Go's goroutines
- Parallel processing of multiple torrent pieces

### 8.4 Load Balancing

- Round-robin DNS for distributing incoming requests across multiple instances
- Consistent hashing for efficient content distribution in clustered deployments

## 9. Security Considerations

### 9.1 Input Validation

All user inputs, including infoHashes and file IDs, are strictly validated to prevent injection attacks.

### 9.2 Rate Limiting

IP-based rate limiting is implemented to prevent abuse and ensure fair usage.

### 9.3 HTTPS

HTTPS is strongly recommended for production deployments. Refer to [HTTPS Setup Guide](docs/https-setup.md) for implementation details.

### 9.4 Access Control

Implement appropriate authentication and authorization mechanisms when deploying in production environments.

## 10. Monitoring and Logging

### 10.1 Logging

tod-p2m uses structured logging with zerolog. Log levels are configurable, and logs can be easily integrated with log aggregation systems like ELK stack or Splunk.

### 10.2 Metrics

Key metrics are exposed via a `/metrics` endpoint in Prometheus format, including:

- Active connections
- Torrents in cache
- Bandwidth usage
- Request latencies

### 10.3 Health Checks

A `/health` endpoint is provided for monitoring the application's health status.

## 11. Testing Strategy

### 11.1 Unit Testing

Run unit tests with:
```bash
go test ./...
```

### 11.2 Integration Testing

Run integration tests with:
```bash
go test -tags=integration ./...
```

### 11.3 Load Testing

Refer to the [Load Testing Guide](docs/load-testing.md) for instructions on performing load tests using tools like Apache JMeter or Gatling.

## 12. Deployment

### 12.1 Containerized Deployment

Refer to the [Docker Deployment Guide](docs/docker-deployment.md) for detailed instructions on deploying tod-p2m using Docker.

## 13. Contributing

We welcome contributions to tod-p2m! Please refer to our [Contributing Guidelines](CONTRIBUTING.md) for details on our code of conduct and the process for submitting pull requests.

## 14. Versioning

We use [SemVer](http://semver.org/) for versioning. For the versions available, see the [tags on this repository](https://github.com/osvalois/tod-p2m/tags).

## 15. License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 16. Acknowledgments

- The [anacrolix/torrent](https://github.com/anacrolix/torrent) library for providing the core BitTorrent functionality.
- The Go community for their excellent libraries and tools.

## 17. Support and Contact

For support, please open an issue in the GitHub repository or contact the maintainers at [osvaloismtz@gmail.com](mailto:osvaloismtz@gmail.com).

---

tod-p2m is continually evolving to meet the demands of modern content delivery. We encourage community feedback and contributions to help make this project even better.