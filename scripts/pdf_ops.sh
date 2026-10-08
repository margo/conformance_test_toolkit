#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# run_pdf_signer.sh
# Wrapper script for pdf_signer.py
# Checks Python installation, installs dependencies, and runs the signer.
# ─────────────────────────────────────────────────────────────────────────────

set -euo pipefail

# ─────────────────────────────────────────────────────────────────────────────
# CONFIGURATION
# ─────────────────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PYTHON_SCRIPT="${SCRIPT_DIR}/pdf_signer.py"
REQUIRED_PYTHON_MAJOR=3
REQUIRED_PYTHON_MINOR=8
DEPENDENCIES=("weasyprint" "pyhanko" "cryptography" "pikepdf")
LOG_FILE="${SCRIPT_DIR}/pdf_signer.log"
OUTPUT_DIR=$(pwd)/generatedReport
PYTHON_CMD=""
VENV_DIR="${SCRIPT_DIR}/.venv"
# Native system libraries required by WeasyPrint, grouped by package manager
WEASYPRINT_APT_DEPS=(
    libpango-1.0-0
    libpangocairo-1.0-0
    libcairo2
    libgdk-pixbuf2.0-0
    libffi-dev
    shared-mime-info
    libglib2.0-0
)
WEASYPRINT_DNF_DEPS=(pango cairo gdk-pixbuf2 libffi)
WEASYPRINT_BREW_DEPS=(pango cairo gdk-pixbuf libffi)
FONTS_DIR="${SCRIPT_DIR}/fonts"
NOTO_EMOJI_FONT="${FONTS_DIR}/NotoColorEmoji.ttf"
NOTO_EMOJI_URL="https://github.com/googlefonts/noto-emoji/raw/main/2D/fonts/NotoColorEmoji.ttf"


# ─────────────────────────────────────────────────────────────────────────────
# LOGGING
# ─────────────────────────────────────────────────────────────────────────────
log() {
    local level="$1"
    shift
    local message="$*"
    local timestamp
    timestamp="$(date '+%Y-%m-%d %H:%M:%S')"
    local formatted="[${timestamp}] [${level}] ${message}"
    echo "${formatted}" | tee -a "${LOG_FILE}"
}

log_info()  { log "INFO " "$@"; }
log_warn()  { log "WARN " "$@"; }
log_error() { log "ERROR" "$@"; }

# ─────────────────────────────────────────────────────────────────────────────
# USAGE
# ─────────────────────────────────────────────────────────────────────────────
usage() {
    echo "Usage: $0 <path_to_html> [path_to_certificate.p12] [passphrase]"
    echo ""
    echo "  path_to_html              Path to the input HTML file (required)"
    echo "  path_to_certificate.p12   Path to an existing .p12 certificate (optional)"
    echo "  passphrase                Passphrase for the .p12 file (optional)"
    echo ""
    echo "Logs are written to: ${LOG_FILE}"
    exit 1
}

# ─────────────────────────────────────────────────────────────────────────────
# STEP 1: Validate arguments
# ─────────────────────────────────────────────────────────────────────────────
validate_args() {
    if [[ $# -lt 1 ]]; then
        log_error "Missing required argument: path_to_html"
        usage
    fi

    local html_path="$1"
    if [[ ! -f "${html_path}" ]]; then
        log_error "HTML file not found: ${html_path}"
        exit 1
    fi

    if [[ $# -ge 2 ]]; then
        local p12_path="$2"
        if [[ ! -f "${p12_path}" ]]; then
            log_error ".p12 certificate file not found: ${p12_path}"
            exit 1
        fi
    fi

    log_info "Arguments validated successfully."
}

# ─────────────────────────────────────────────────────────────────────────────
# STEP 2: Check Python installation
# ─────────────────────────────────────────────────────────────────────────────


check_python() {
    log_info "Checking Python installation..."

    local python_cmd=""
    for cmd in python3 python; do
        if command -v "${cmd}" &>/dev/null; then
            python_cmd="${cmd}"
            break
        fi
    done

    if [[ -z "${python_cmd}" ]]; then
        log_error "Python is not installed or not in PATH."
        log_error "Install Python ${REQUIRED_PYTHON_MAJOR}.${REQUIRED_PYTHON_MINOR}+ from https://www.python.org/downloads/"
        exit 1
    fi

    local version
    version="$("${python_cmd}" -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')"
    local major minor
    major="$(echo "${version}" | cut -d. -f1)"
    minor="$(echo "${version}" | cut -d. -f2)"

    if [[ "${major}" -lt "${REQUIRED_PYTHON_MAJOR}" ]] || \
       [[ "${major}" -eq "${REQUIRED_PYTHON_MAJOR}" && "${minor}" -lt "${REQUIRED_PYTHON_MINOR}" ]]; then
        log_error "Python ${version} detected. Minimum required: ${REQUIRED_PYTHON_MAJOR}.${REQUIRED_PYTHON_MINOR}"
        exit 1
    fi

    log_info "Python ${version} detected at: $(command -v "${python_cmd}")"

    # Set global instead of echo-returning
    PYTHON_CMD="${python_cmd}"
}

# ─────────────────────────────────────────────────────────────────────────────
# STEP 3: Check pip availability
# ─────────────────────────────────────────────────────────────────────────────
check_pip() {
    local python_cmd="$1"
    log_info "Checking pip availability..."

    if ! "${python_cmd}" -m pip --version &>/dev/null; then
        log_error "pip is not available. Install it with: ${python_cmd} -m ensurepip --upgrade"
        exit 1
    fi

    log_info "pip is available: $("${python_cmd}" -m pip --version)"
}

# ─────────────────────────────────────────────────────────────────────────────
# STEP 4: Install WeasyPrint native system libraries
# Detects the OS and package manager, installs only missing packages.
# Non-fatal if sudo is unavailable — emits a warning and continues.
# ─────────────────────────────────────────────────────────────────────────────
install_system_deps() {
    log_info "Checking WeasyPrint native system dependencies..."

    # ── macOS (Homebrew) ──────────────────────────────────────────────────────
    if [[ "${OSTYPE}" == "darwin"* ]]; then
        log_info "macOS detected — using Homebrew."

        if ! command -v brew &>/dev/null; then
            log_error "Homebrew not found. Install it from https://brew.sh and re-run."
            exit 1
        fi

        local missing_brew=()
        for pkg in "${WEASYPRINT_BREW_DEPS[@]}"; do
            if ! brew list --formula 2>/dev/null | grep -q "^${pkg}$"; then
                missing_brew+=("${pkg}")
            fi
        done

        if [[ ${#missing_brew[@]} -eq 0 ]]; then
            log_info "All Homebrew dependencies already installed."
        else
            log_info "Installing missing Homebrew packages: ${missing_brew[*]}"
            if brew install "${missing_brew[@]}" >> "${LOG_FILE}" 2>&1; then
                log_info "Homebrew packages installed successfully."
            else
                log_error "Failed to install Homebrew packages. Check ${LOG_FILE} for details."
                exit 1
            fi
        fi

        # Ensure Homebrew libs are on the dynamic linker path for this session
        export DYLD_LIBRARY_PATH="$(brew --prefix)/lib:${DYLD_LIBRARY_PATH:-}"
        log_info "DYLD_LIBRARY_PATH updated to include: $(brew --prefix)/lib"
        return 0
    fi

    # ── Linux ─────────────────────────────────────────────────────────────────
    if [[ "${OSTYPE}" == "linux-gnu"* || "${OSTYPE}" == "linux"* ]]; then

        local sudo_cmd=""
        if [[ "${EUID}" -ne 0 ]]; then
            if command -v sudo &>/dev/null; then
                sudo_cmd="sudo"
            else
                log_warn "Not running as root and sudo is unavailable."
                log_warn "Skipping system dependency installation — WeasyPrint may fail to load."
                log_warn "Manually install the required libraries and re-run this script."
                return 0
            fi
        fi

        # ── apt (Debian / Ubuntu) ─────────────────────────────────────────────
        if command -v apt-get &>/dev/null; then
            log_info "Debian/Ubuntu detected — using apt-get."

            local missing_apt=()
            for pkg in "${WEASYPRINT_APT_DEPS[@]}"; do
                if ! dpkg-query -W -f='${Status}' "${pkg}" 2>/dev/null \
                        | grep -q "install ok installed"; then
                    missing_apt+=("${pkg}")
                fi
            done

            if [[ ${#missing_apt[@]} -eq 0 ]]; then
                log_info "All apt dependencies already installed."
                return 0
            fi

            log_info "Installing missing apt packages: ${missing_apt[*]}"
            if ! ${sudo_cmd} apt-get update -qq >> "${LOG_FILE}" 2>&1; then
                log_warn "apt-get update failed — attempting install anyway."
            fi

            if ${sudo_cmd} apt-get install -y "${missing_apt[@]}" >> "${LOG_FILE}" 2>&1; then
                log_info "apt packages installed successfully."
                ${sudo_cmd} ldconfig >> "${LOG_FILE}" 2>&1 || true
            else
                log_error "apt-get install failed. Check ${LOG_FILE} for details."
                exit 1
            fi
            return 0
        fi

        # ── dnf (Fedora / RHEL 8+) ────────────────────────────────────────────
        if command -v dnf &>/dev/null; then
            log_info "Fedora/RHEL detected — using dnf."

            local missing_dnf=()
            for pkg in "${WEASYPRINT_DNF_DEPS[@]}"; do
                rpm -q "${pkg}" &>/dev/null || missing_dnf+=("${pkg}")
            done

            if [[ ${#missing_dnf[@]} -eq 0 ]]; then
                log_info "All dnf dependencies already installed."
                return 0
            fi

            log_info "Installing missing dnf packages: ${missing_dnf[*]}"
            if ${sudo_cmd} dnf install -y "${missing_dnf[@]}" >> "${LOG_FILE}" 2>&1; then
                log_info "dnf packages installed successfully."
                ${sudo_cmd} ldconfig >> "${LOG_FILE}" 2>&1 || true
            else
                log_error "dnf install failed. Check ${LOG_FILE} for details."
                exit 1
            fi
            return 0
        fi

        # ── yum (CentOS / RHEL 7) ─────────────────────────────────────────────
        if command -v yum &>/dev/null; then
            log_info "CentOS/RHEL detected — using yum."

            local missing_yum=()
            for pkg in "${WEASYPRINT_DNF_DEPS[@]}"; do
                rpm -q "${pkg}" &>/dev/null || missing_yum+=("${pkg}")
            done

            if [[ ${#missing_yum[@]} -eq 0 ]]; then
                log_info "All yum dependencies already installed."
                return 0
            fi

            log_info "Installing missing yum packages: ${missing_yum[*]}"
            if ${sudo_cmd} yum install -y "${missing_yum[@]}" >> "${LOG_FILE}" 2>&1; then
                log_info "yum packages installed successfully."
                ${sudo_cmd} ldconfig >> "${LOG_FILE}" 2>&1 || true
            else
                log_error "yum install failed. Check ${LOG_FILE} for details."
                exit 1
            fi
            return 0
        fi

        log_error "No supported package manager found (apt-get / dnf / yum)."
        log_error "Manually install: libpango, libcairo2, libgdk-pixbuf2, libffi"
        exit 1
    fi

    # ── Unsupported OS ────────────────────────────────────────────────────────
    log_warn "Unsupported OS: ${OSTYPE}. Cannot install system dependencies automatically."
    log_warn "See: https://doc.courtbouillon.org/weasyprint/stable/first_steps.html"
}

# ─────────────────────────────────────────────────────────────────────────────
# STEP 5: Download Noto Emoji font for PDF embedding
# ─────────────────────────────────────────────────────────────────────────────

setup_emoji_font() {
    log_info "Checking Noto Emoji font..."
    sudo apt-get install fonts-noto-color-emoji -y
}

# ─────────────────────────────────────────────────────────────────────────────
# STEP 6: Setup virtual environment
# ─────────────────────────────────────────────────────────────────────────────
setup_venv() {
    log_info "Setting up virtual environment at: ${VENV_DIR}"

    if [[ ! -d "${VENV_DIR}" ]]; then
        log_info "Creating new virtual environment..."
        if ! "${PYTHON_CMD}" -m venv "${VENV_DIR}" >> "${LOG_FILE}" 2>&1; then
            log_error "Failed to create virtual environment."
            log_error "Ensure python3-full is installed: sudo apt install python3-full"
            exit 1
        fi
        log_info "Virtual environment created successfully."
    else
        log_info "Virtual environment already exists, reusing it."
    fi

    # Switch PYTHON_CMD to the venv's Python
    PYTHON_CMD="${VENV_DIR}/bin/python"
    log_info "Using venv Python: ${PYTHON_CMD}"
}

# ─────────────────────────────────────────────────────────────────────────────
# STEP 7: Install dependencies (unchanged signature, now runs inside venv)
# ─────────────────────────────────────────────────────────────────────────────
install_dependencies() {
    log_info "Checking and installing required dependencies..."

    # Upgrade pip inside venv first
    "${PYTHON_CMD}" -m pip install --upgrade pip >> "${LOG_FILE}" 2>&1

    for dep in "${DEPENDENCIES[@]}"; do
        if "${PYTHON_CMD}" -m pip show "${dep}" &>/dev/null; then
            log_info "Dependency already installed: ${dep}"
        else
            log_info "Installing missing dependency: ${dep}"
            if "${PYTHON_CMD}" -m pip install "${dep}" >> "${LOG_FILE}" 2>&1; then
                log_info "Successfully installed: ${dep}"
            else
                log_error "Failed to install: ${dep}. Check ${LOG_FILE} for details."
                exit 1
            fi
        fi
    done

    log_info "All dependencies are satisfied."
}



# ─────────────────────────────────────────────────────────────────────────────
# STEP 8: Validate the Python script exists
# ─────────────────────────────────────────────────────────────────────────────
check_python_script() {
    if [[ ! -f "${PYTHON_SCRIPT}" ]]; then
        log_error "Python script not found: ${PYTHON_SCRIPT}"
        exit 1
    fi
    log_info "Python script found: ${PYTHON_SCRIPT}"
}

# ─────────────────────────────────────────────────────────────────────────────
# STEP 9: Run the Python script
# ─────────────────────────────────────────────────────────────────────────────
run_pdf_signer() {
    local python_cmd="$1"
    shift
    log_info "Running pdf_signer.py with args: $*"
    log_info "────────────────────────────────────────"

    if "${python_cmd}" "${PYTHON_SCRIPT}" "$@" 2>&1 | tee -a "${LOG_FILE}"; then
        log_info "────────────────────────────────────────"
        log_info "pdf_signer.py completed successfully."
    else
        log_error "────────────────────────────────────────"
        log_error "pdf_signer.py exited with an error. Check ${LOG_FILE} for details."
        exit 1
    fi
}

# ─────────────────────────────────────────────────────────────────────────────
# MAIN
# ─────────────────────────────────────────────────────────────────────────────
main() {
    log_info "========================================"
    log_info "PDF Signer Wrapper Script Started"
    log_info "========================================"

    validate_args "$@"

    local html_path="$1"
    local cert_path="${2:-}"
    local passphrase="${3:-}"

    check_python
    check_pip "${PYTHON_CMD}"
    install_system_deps
    setup_venv
    install_dependencies
    check_python_script
    setup_emoji_font

    mkdir -p "${OUTPUT_DIR}"

    # Build python args: only html is positional; cert and passphrase are named flags
    local py_args=("${html_path}" --output-dir "${OUTPUT_DIR}")
    if [[ -n "${cert_path}" ]]; then
        py_args+=(--cert "${cert_path}")
    fi
    if [[ -n "${passphrase}" ]]; then
        py_args+=(--passphrase "${passphrase}")
    fi

    run_pdf_signer "${PYTHON_CMD}" "${py_args[@]}"

    log_info "========================================"
    log_info "PDF Signer Wrapper Script Finished"
    log_info "========================================"
}

main "$@"