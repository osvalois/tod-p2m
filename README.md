# tod-p2m

## Overview

This application is a **tod-p2m** built in Go, designed to stream video and audio content directly from torrent files. The API provides endpoints to interact with torrent files, retrieve torrent metadata, and serve media content via HTTP streaming, including HLS (HTTP Live Streaming) for segmented media delivery.

## Features

- **Magnet Link Support**: Automatically fetches and streams content from magnet links.
- **HTTP Streaming**: Streams torrent files over HTTP with support for byte-range requests.
- **HLS Streaming**: Provides HLS playlists and segmented `.ts` files for adaptive streaming.
- **Rate Limiting**: Configurable download and upload rate limits.
- **Cache Management**: Built-in cache and cleanup system for managing torrent lifecycle.
- **Customizable Configuration**: Fully configurable through environment variables and configuration files.
- **Cross-Origin Resource Sharing (CORS)**: CORS-enabled API to allow content access from multiple sources.

## Architecture

This application uses the **Go-Chi** router for request handling and **anacrolix/torrent** for torrent client management. It is designed with performance and scalability in mind, capable of handling concurrent connections and efficient memory usage for streaming large files.

### Key Components:

- **Torrent Client**: Handles torrent fetching, peer communication, and data streaming.
- **HTTP API**: Serves torrent metadata and media files using byte-range or HLS.
- **Cache System**: Ensures efficient memory and file system usage, removing inactive or completed torrents.

## Requirements

- **Go** (v1.18+)
- **Config File**: `config.yaml` to customize the application's behavior.

### Example `config.yaml`

```yaml
port: "8080"
log_level: "info"
torrent_timeout: 30s
max_connections: 100
download_rate_limit: 0
upload_rate_limit: 0
cache_size: 100
cleanup_interval: 10m
hls_segment_duration: 10
```

## Installation

1. **Clone the Repository**

   ```bash
   git clone https://github.com/osvalois/tod-p2m.git
   ```

2. **Navigate to the Project Directory**

   ```bash
   cd tod-p2m
   ```

3. **Install Dependencies**

   Ensure you have Go installed, then install the required packages:

   ```bash
   go mod download
   ```

4. **Configure the Application**

   Create or modify the `config.yaml` file in the root directory with your custom settings, or use the default settings.

5. **Run the Application**

   Start the server:

   ```bash
   go run main.go
   ```

6. **Build the Application** (optional)

   You can build the project into a binary:

   ```bash
   go build -o tod-p2m
   ```

## API Endpoints

### 1. **Get Torrent Info**
   Retrieves metadata of the torrent, including file details.

   **Endpoint**: `GET /torrent/{infoHash}`

   **Response**:
   ```json
   {
     "infoHash": "1234567890abcdef...",
     "name": "Torrent Name",
     "files": [
       {
         "id": 0,
         "name": "file1.mp4",
         "size": 104857600,
         "progress": 0.56
       }
     ]
   }
   ```

### 2. **Stream File**
   Streams a specific file from a torrent using byte-range requests.

   **Endpoint**: `GET /stream/{infoHash}/{fileID}`

   **Headers**:
   - `Range`: Optional for partial file requests.

### 3. **HLS Playlist**
   Provides an HLS playlist for a video or audio file in the torrent.

   **Endpoint**: `GET /hls/{infoHash}/{fileID}/playlist.m3u8`

   **Response**: Returns the HLS playlist in `.m3u8` format.

### 4. **HLS Segment**
   Provides a specific `.ts` segment of a video file for HLS streaming.

   **Endpoint**: `GET /hls/{infoHash}/{fileID}/{segmentID}.ts`

## Configuration

The application can be configured using a YAML file or environment variables.

- **port**: The port on which the API will run.
- **log_level**: Log level for the application (e.g., `info`, `debug`).
- **torrent_timeout**: Timeout for fetching torrent metadata.
- **max_connections**: Maximum concurrent connections.
- **download_rate_limit**: Rate limit for downloading in bytes per second (0 means unlimited).
- **upload_rate_limit**: Rate limit for uploading in bytes per second (0 means unlimited).
- **cache_size**: Maximum number of torrents to cache in memory.
- **cleanup_interval**: Interval for cleaning up inactive torrents.
- **hls_segment_duration**: Duration in seconds for each HLS segment.

## Error Handling

All errors are returned in JSON format with appropriate HTTP status codes. Example:

```json
{
  "error": "File not found"
}
```

## Logging

The application uses **zerolog** for structured and high-performance logging. The log level can be set in the configuration file to control verbosity.

## Performance Optimization

- **Rate Limiting**: Configurable download and upload limits for controlling bandwidth.
- **Torrent Caching**: Inactive torrents are automatically cleaned up to free resources.
- **Concurrency**: Designed to handle high levels of concurrent connections without significant performance degradation.

## Contributing

Contributions are welcome! Please fork the repository and submit a pull request with your proposed changes.

1. Fork the project
2. Create your feature branch (`git checkout -b feature/your-feature`)
3. Commit your changes (`git commit -m 'Add some feature'`)
4. Push to the branch (`git push origin feature/your-feature`)
5. Open a pull request

## License

This project is licensed under the **MIT License**. See the [LICENSE](LICENSE) file for details.

## Contact

For any inquiries or support, feel free to reach out to [your-email@example.com](mailto:osvaloismtz@gmail.com").

---

This application is designed with scalability and flexibility in mind, ensuring smooth media streaming and efficient torrent management. Happy streaming!