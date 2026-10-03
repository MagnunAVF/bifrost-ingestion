# Project Bifröst Ingestion

## Description

Roadmap with project milestones and issues.

## Project Goals

- Catalog ingestion;
- Deduplication pipeline;

## Milestone 1: Local MVP

The core deduplication logic implemented as a procedural Go script. It uses in-memory vector math and local LLM embeddings (Ollama) to fit tight hardware constraints while ensuring data integrity.

- **Issue 1.1: Database Connection and Schema Migration:** Initialize the Go module, connect to SQLite, and migrate the relationship table's foreign key column to accept UUID strings instead of strictly typed integers.
- **Issue 1.2: JSON Parsing and Data Sanitization:** Parse incoming catalog payloads. Implement defensive routines to handle null attributes, normalize spacing, neutralize SQL injections, and output a clean, concatenated semantic string.
- **Issue 1.3: Local Embedding Integration:** Build a lightweight Go HTTP client to interface with a local Ollama instance, converting sanitized product strings into high-dimensional vector arrays.
- **Issue 1.4: In-Memory Similarity Engine:** Write a Cosine Similarity function in pure Go. Perform a "cold start" by pulling all existing catalog items, generating their embeddings, and storing them in a rapid-access RAM map.
- **Issue 1.5: Upsert Decision Logic:** Compare incoming product vectors against the in-memory catalog. Products above the similarity threshold are linked to the existing entry; novel items are inserted into the database and added to the memory map.
- **Issue 1.6: Executive Documentation:** Draft a comprehensive README detailing the system architecture, engineering trade-offs, hardware optimizations, and defensive sanitization strategies.

## Milestone 2: Intelligent Decision Engine

Transitioning from hardcoded mathematical thresholds to a dynamic cognitive architecture, delegating ambiguous edge cases to a specialized Python AI agent.

- **Issue 2.1: Go MCP Server Implementation:** Upgrade the Go pipeline to function as a Model Context Protocol (MCP) server, exposing tools for external agents to safely query and insert database records.
- **Issue 2.2: Python LangChain Agent:** Develop a Python LangChain application to act as the cognitive engine. It connects to the Go MCP server and uses an LLM to evaluate complex, ambiguous product variations.
- **Issue 2.3: Hybrid Orchestration Workflow:** Wire the Go ingestor to act as a triage layer. Clear-cut matches are processed instantly via math, while borderline cases are routed to the Python agent for an authoritative, contextual decision.

## Milestone 3: API Server

Wrapping the standalone ingestion scripts into a secure, network-accessible layer to act as a true gateway for external catalog submissions.

- **Issue 3.1: HTTP API Gateway:** Set up a fast, non-blocking web server using the Go standard library to expose RESTful endpoints for catalog uploads.
- **Issue 3.2: Streaming Uploads:** Implement a multipart form-data streaming endpoint to process large JSON payloads item-by-item, preventing memory exhaustion on constrained hardware.
- **Issue 3.3: Clean Architecture Refactoring:** Restructure the codebase into defined layers (Handlers, Services, Repositories, and MCP Interfaces) to ensure maintainability and testability.

## Milestone 4: Asynchronous Processing and Backpressure

Introducing asynchronous workflows and queuing mechanisms to protect the embedding engine and database from traffic spikes.

- **Issue 4.1: Concurrent Worker Pools:** Refactor the processing loop to utilize Goroutines, allowing the application to vectorize and persist multiple products concurrently.
- **Issue 4.2: Message Broker Integration:** Introduce a message queue (e.g., RabbitMQ). The HTTP endpoint instantly returns a success response while publishing the payload to a queue for background workers to consume at a governed pace.
- **Issue 4.3: Batch Inference Optimization:** Group incoming products into batches before sending them to the embedding engine, reducing network overhead and maximizing hardware parallelization.

## Milestone 5: Cloud-Native Production

Preparing the Bifröst system for global deployment by replacing local components with enterprise-grade distributed infrastructure.

- **Issue 5.1: Vector Datastore Migration:** Replace local SQLite and in-memory calculations with a distributed, highly scalable vector database (options: Weaviate or PostgreSQL with pgvector).
- **Issue 5.2: Multi-Stage Containerization:** Write optimized, secure multi-stage Dockerfiles for both the Go backend and the Python cognitive agent.
- **Issue 5.3: Kubernetes Orchestration:** Develop Kubernetes manifests (Deployments, Services, Horizontal Pod Autoscalers) to dynamically scale worker pods based on real-time ingestion queue metrics.
- **Issue 5.4: Telemetry and Observability:** Instrument all components with OpenTelemetry to export operational metrics (duplicate rates, inference latency, agent reasoning times) to Prometheus and Grafana dashboards.
