# How to setup wfm?
export GITHUB_USER=<your-github-username>
export GITHUB_TOKEN=<your-github-token>
export SYMPHONY_BRANCH=margo-dev-sprint-6
export DEV_REPO_BRANCH=dev-sprint-6
sudo -E bash wfm.sh

# How to setup device agent?
export GITHUB_USER=<your-github-username>
export GITHUB_TOKEN=<your-github-token>
export SYMPHONY_BRANCH=margo-dev-sprint-6
export WFM_IP=127.0.0.1
export WFM_PORT=8082
sudo -E bash device-agent.sh