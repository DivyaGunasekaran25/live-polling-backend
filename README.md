# 🔴 Live Polling Backend

Go-based backend for a real-time live polling application.

The backend provides REST APIs for authentication, poll creation, voting, and real-time poll result updates using MongoDB, Redis Pub/Sub, and Server-Sent Events (SSE).

## 🚀 Features

- 🔐 User signup and login
- 🔑 Token-based authentication
- 📝 Poll creation
- 📋 Poll retrieval
- 🗳️ Voting API
- 📊 Vote count management
- 🔴 Real-time poll updates
- 📡 Server-Sent Events (SSE)
- ⚡ Redis Pub/Sub for real-time communication
- 🗄️ MongoDB database integration
- 🔒 Password hashing using bcrypt
- 🌐 CORS support

## 🛠️ Technology Stack

- Go
- Gin Web Framework
- MongoDB
- Redis
- Server-Sent Events (SSE)
- bcrypt
- REST API

## 🏗️ Backend Architecture

```text
                  ┌───────────────────┐
                  │   React Frontend  │
                  └─────────┬─────────┘
                            │
                       HTTP / SSE
                            │
                            ▼
                  ┌───────────────────┐
                  │    Go + Gin API   │
                  └───────┬─────┬─────┘
                          │     │
                ┌─────────▼─┐ ┌─▼──────────┐
                │  MongoDB  │ │   Redis    │
                │ Database  │ │  Pub/Sub   │
                └───────────┘ └─────┬──────┘
                                    │
                              Live Events
                                    │
                                    ▼
                              SSE Clients
## Related Repository

### Frontend
The frontend application is available here:

[Live Polling Frontend](https://github.com/DivyaGunasekaran25/live-polling-frontend)
