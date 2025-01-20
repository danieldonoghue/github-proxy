# GitHub Proxy

## Overview

GitHub Proxy is a tool designed to act as an intermediary between your application and GitHub's API. It simplifies the process of interacting with GitHub by providing a set of proxy endpoints.

GitHub Proxy was intended to allow Private GitHub repositories to be used as Debian apt repositories.

## Setup

To set up the GitHub Proxy, follow these steps:

1. Clone the repository:
    ```sh
    git clone https://github.com/yourusername/github-proxy.git
    cd github-proxy
    ```

2. Install dependencies:
    ```sh
    go mod tidy
    ```

3. Build the project:
    ```sh
    go build -o github-proxy cmd/github-proxy/
    ```

4. Run the proxy:
    ```sh
    ./github-proxy -client-id <your-github-app-client-id> -installation-id <your-github-app-installation-id> [options]
    ```

## GitHub integration

To Use Github-Proxy, you will need a GitHub App installation that can interact with one or more repoositories. 

The Github App should have the following repository permissions:
* Metadata *(read-only)* 
* Contents *(read-only)* 

You will also need to generate a private key and download it!

## Usage 
Once the proxy is running, you can interact with it the same was as using GitHub's own web interface.

For example: `curl -s http://localhost:8080/repo-owner/repo/file` would attempt to download `file` from the `repo` repo, owned by `repo-owner` (assuming said repo/owner had a relevant GitHub App installed) via github-proxy, running on `localhost:8080`.

#### Usage of github-proxy
```
  -bind string
        Address to bind the server to (default ":8080")
  -client-id string
    	GitHub App client ID
  -installation-id string
    	GitHub App installation ID
  -private-key string
    	Path to the GitHub App private key file
  -use-vault
    	Use HashiCorp Vault to retrieve the private key
  -version
    	Print the version and exit
```

WHERE:
* `bind` - the local address to listen on for incoming requests
* `client-id` - the Client ID for your GitHub App
* `installation-id` - the Installation ID for your GitHub App
* `private-key` is either:
    * the file path to the PEM file for your GitHub App
    * the path in HahiCorp Vault to a secret containing the PEM file for your GitHub App. This path is in the format: `<mount-point>/<path>[:<field>]`; `field` defaults to `private_key`.
* `use-vault` - treat the `private-key` as a path in vault instead of a path on the file system.

#### Environment Variables

* The usual `VAULT_` environment variables will be used if you are using Vault.
* `GH_PRIVATE_KEY` - can contain the raw Github App Private key. This will be checked if you omit the `private-key` and `use-vault` arguments.

---

## Triggering GitHub Action Workflow for Releasing

This project includes a GitHub Action workflow for releasing new versions. To trigger the workflow:

1. Create a new tag for the release:
    ```sh
    git tag -a v1.0.0 -m "Release version 1.0.0"
    git push origin v1.0.0
    ```

2. The GitHub Action workflow will automatically run and create a new release based on the tag.

For more details, refer to the `.github/workflows/build-and-release.yml` file in the repository.

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

## License

This project is licensed under the MIT License.
