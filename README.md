# Scrum Poker

[![Badge: Linux](https://img.shields.io/badge/Linux-FCC624.svg?logo=linux&logoColor=black)](#readme)
[![Badge: Terraform](https://img.shields.io/badge/Terraform-%235835CC.svg?logo=terraform&logoColor=white)](#readme)
[![Badge: Go](https://img.shields.io/badge/Go-%2300ADD8.svg?logo=go&logoColor=white)](#readme)
[![Badge: Docker](https://img.shields.io/badge/Docker-%230db7ed.svg?logo=docker&logoColor=white)](#readme)
[![Badge: Podman](https://img.shields.io/badge/Podman-%23892CA0.svg?logo=podman&logoColor=white)](#readme)
[![Badge: Kubernetes](https://img.shields.io/badge/Kubernetes-%23326ce5.svg?logo=kubernetes&logoColor=white)](#readme)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)

**Scrum Poker** is a lightweight, real-time estimation tool for Agile teams. It simplifies the sprint planning process with a clean, no-frills interface and instant feedback.

![Screenshot](./img/screenshot.webp)

## Features

-   **Real-time Updates**: Uses Server-Sent Events (SSE) for instant vote visibility without refreshing.
-   **No Registration**: Jump straight into a room with a generated session ID.
-   **Ad-Hoc Rooms**: Create a room instantly; rooms persist in memory (flushed to disk on shutdown) and are stored for a maximum of one day.
-   **Serverless Ready**: Tested and optimized for Google Cloud Run; runs within free tier limits for small teams.
-   **Dealer Controls**: The room creator (Game Master, Dealer) has exclusive rights to **Reveal Cards** and **Reset Votes**.
-   **Standard Deck**: Includes Fibonacci sequence (0, 1, 2, 3, 5, 8, 13, 21), `?` (unsure), and `☕` (break).
-   **Mobile Friendly**: Responsive design works seamlessly on desktop and mobile.

## Getting Started

This application is developed and optimized for Google Cloud Platform, leveraging Cloud Run for serverless container execution (with scale-to-zero for cost efficiency) and Google Cloud Storage Buckets for persistent state management. The bucket has a lifecycle policy configured to automatically delete room data older than one day. Note that this automatic cleanup lifecycle policy only works when running in GCP.

For local development and testing, the application is fully containerized and supports Docker, Podman, and Kubernetes deployments. You can also run it directly with Go.

### Google Cloud Run Deployment

[![Run on Google Cloud](https://deploy.cloud.run/button.svg)](./gcp/README.md)

For more information, please see the [`gcp/README.md`](./gcp/README.md).

### Containerization with Docker or Podman

This project supports containerization with either [Docker](https://www.docker.com/) or [Podman](https://podman.io/).
The instructions and scripts have been tested with Podman.

### Build the Container Image

Alternatively, you can use the `tools/build-podman.sh` script to automate this process with Podman.

1.  **Build the image**:

    ```bash
    docker build -t scrumpoker .
    ```

2.  **Run the container**:

    ```bash
    docker run -d -p 8080:8080 --name scrumpoker scrumpoker
    ```

    The application will then be accessible at `http://localhost:8080`.

### Docker Compose

Alternatively, you can use docker-compose to manage the application.

Then run:

```bash
docker-compose up -d --build
```

### Local Development

Prerequisites: [Go 1.25+](https://go.dev/)

1.  **Clone the repository**:

    ```bash
    git clone https://github.com/Cyclenerd/scrumpoker.git
    cd scrumpoker
    ```

2.  **Run the application**:

    ```bash
    go run main.go
    ```

    The server will start on `http://localhost:8080`.

## Usage

1.  **Create a Room**: Go to the homepage and click "Create Room". You become the **Dealer**.
2.  **Invite Team**: Share the URL or Room ID with your team.
3.  **Vote**: Players select a card. Their status changes to "Voted" (card remains hidden).
4.  **Reveal**: The Dealer clicks "Reveal Cards" to show all estimates.
5.  **Reset**: The Dealer clicks "Start New Round" to clear votes for the next story.

## Contributing

Contributions are welcome! Please read our [Contributing Guidelines](CONTRIBUTING.md) for details on our code of conduct and the process for submitting pull requests.

## Credits

All background animations are based on GLSL shader animation created by [KF2015](https://www.youtube.com/@KF-2015) found on [YouTube](https://www.youtube.com/watch?v=7GutRIhmTwQ). The code was adapted to run in a browser using WebGL.

**Attribution Requirement**: If you re-use one of the animations, please credit KF2015!

## License

This project is licensed under the **AGPL v3 License**. See the [LICENSE](LICENSE) file for details.
