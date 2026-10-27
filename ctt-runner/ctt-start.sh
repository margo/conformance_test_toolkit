#!/bin/bash

################################################################################
# Margo Conformance Test Runner (CLI #2)
#
# Purpose: Execute the test groups created with ctt-creator/ctt-start.sh (CLI #1)
#          - WFM Supplier: sends the group's requests to a real WFM over mTLS
#          - Device Supplier: runs the mock WFM and the group's scenarios
#          - Application Supplier: validates an Application Package
#          - Generates the HTML conformance report and its signed PDF
#
# Story: #278 - "As a Margo adopter, I would like to have a tool to allow me
#        to select the suitable persona and run a set of conformance test-cases
#        to get a signed report of conformance"
#
# QUICKSTART: docs/wfm-supplier-setup-guide.md, docs/device-supplier-setup-guide.md
#
################################################################################

set -euo pipefail

################################################################################
# Configuration
################################################################################

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFORMANCE_DIR="$SCRIPT_DIR"  # this script is already in the ctt-runner directory
DATA_GEN_DIR="$SCRIPT_DIR/../ctt-creator"
RUNNER_DIR="$CONFORMANCE_DIR/reports"  # Output directory for test results
WFM_GROUP_DIR="$SCRIPT_DIR/../test-suites/wfm-supplier"
DEVICE_GROUP_DIR="$SCRIPT_DIR/../test-suites/device-supplier"
APPLICATION_DIR="$CONFORMANCE_DIR/../test-suites/app-supplier"
APPLICATION_SERVICE_DIR="$CONFORMANCE_DIR/app-supplier/scripts"

# Create output directories
RUNNER_WFM="$RUNNER_DIR/wfm-supplier"
RUNNER_DEVICE="$RUNNER_DIR/device-supplier"
mkdir -p "$RUNNER_WFM" "$RUNNER_DEVICE"

################################################################################
# Logging Functions
################################################################################

log() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] 📝 $*"
}

info() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] ℹ️  $*"
}

success() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] ✅ $*"
}

error() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] ❌ $*" >&2
    exit 1
}

warn() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] ⚠️  $*" >&2
}

################################################################################
# Prerequisite Check / Install
################################################################################

# Maps a tool name to the package name for the detected package manager.
# Echoes "" if the tool/manager combo isn't recognized.
_prereq_pkg_name() {
    local mgr="$1" tool="$2"
    case "$mgr:$tool" in
        apt-get:go) echo "golang-go" ;;
        dnf:go|yum:go) echo "golang" ;;
        apk:go) echo "go" ;;
        brew:go) echo "go" ;;
        apt-get:jq|dnf:jq|yum:jq|apk:jq|brew:jq) echo "jq" ;;
        apt-get:openssl|dnf:openssl|yum:openssl|apk:openssl|brew:openssl) echo "openssl" ;;
        apt-get:node|dnf:node|yum:node|apk:node) echo "nodejs npm" ;;
        brew:node) echo "node" ;;
        *) echo "" ;;
    esac
}

# check_prerequisites detects the tools required by either persona:
#   go, jq, openssl, node/npm (WFM's scenario runner + Newman), newman itself.
# Reports what's missing and offers to install it via the detected system
# package manager (+ npm for newman). Safe to run multiple times — only
# touches packages that are actually missing, and never runs unprompted.
check_prerequisites() {
    echo ""
    info "Checking prerequisites for WFM Supplier + Device Supplier personas..."
    echo ""

    local pkg_manager=""
    if command -v apt-get >/dev/null 2>&1; then
        pkg_manager="apt-get"
    elif command -v dnf >/dev/null 2>&1; then
        pkg_manager="dnf"
    elif command -v yum >/dev/null 2>&1; then
        pkg_manager="yum"
    elif command -v apk >/dev/null 2>&1; then
        pkg_manager="apk"
    elif command -v brew >/dev/null 2>&1; then
        pkg_manager="brew"
    fi

    local -a missing_tools=()
    local -a missing_pkgs=()
    local tool pkg
    for tool in go jq openssl node; do
        if ! command -v "$tool" >/dev/null 2>&1; then
            missing_tools+=("$tool")
            if [[ -n "$pkg_manager" ]]; then
                pkg=$(_prereq_pkg_name "$pkg_manager" "$tool")
                # Intentionally unquoted: some entries (e.g. "nodejs npm") are
                # two package names and must split into separate array items.
                [[ -n "$pkg" ]] && missing_pkgs+=($pkg)
            fi
        fi
    done

    local newman_missing=false
    if ! command -v newman >/dev/null 2>&1; then
        newman_missing=true
        missing_tools+=("newman")
    fi

    if [[ ${#missing_tools[@]} -eq 0 ]]; then
        success "All prerequisites are already installed (go, jq, openssl, node, newman)."
        return 0
    fi

    warn "Missing prerequisites: ${missing_tools[*]}"

    echo ""
    read -p "Install missing prerequisites now? [y/N]: " confirm < /dev/tty
    if [[ ! "${confirm,,}" =~ ^y ]]; then
        warn "Skipped. Re-run this option any time, or install manually:"
        echo "    go       → https://golang.org/doc/install"
        echo "    jq       → install via your package manager (apt/dnf/yum/apk/brew install jq)"
        echo "    openssl  → install via your package manager (apt/dnf/yum/apk/brew install openssl)"
        echo "    node/npm → https://nodejs.org (or your package manager's nodejs/npm packages)"
        echo "    newman   → npm install -g newman newman-reporter-htmlextra"
        return 1
    fi

    local sudo_cmd=""
    [[ "$(id -u)" -ne 0 ]] && sudo_cmd="sudo"

    if [[ ${#missing_pkgs[@]} -gt 0 ]]; then
        if [[ -z "$pkg_manager" ]]; then
            warn "No supported package manager found (apt-get/dnf/yum/apk/brew) — install go/jq/openssl/node manually (see links above)."
        else
            info "Installing via $pkg_manager: ${missing_pkgs[*]}"
            case "$pkg_manager" in
                apt-get)
                    $sudo_cmd apt-get update && $sudo_cmd apt-get install -y "${missing_pkgs[@]}" || warn "apt-get install failed — see errors above."
                    ;;
                dnf)
                    $sudo_cmd dnf install -y "${missing_pkgs[@]}" || warn "dnf install failed — see errors above."
                    ;;
                yum)
                    $sudo_cmd yum install -y "${missing_pkgs[@]}" || warn "yum install failed — see errors above."
                    ;;
                apk)
                    $sudo_cmd apk add "${missing_pkgs[@]}" || warn "apk add failed — see errors above."
                    ;;
                brew)
                    brew install "${missing_pkgs[@]}" || warn "brew install failed — see errors above."
                    ;;
            esac
        fi
    fi

    if $newman_missing; then
        if command -v npm >/dev/null 2>&1; then
            info "Installing newman + newman-reporter-htmlextra via npm..."
            npm install -g newman newman-reporter-htmlextra || warn "npm install -g failed — try: sudo npm install -g newman newman-reporter-htmlextra"
        else
            warn "npm still not available — cannot install newman yet. Install Node.js first, then run: npm install -g newman newman-reporter-htmlextra"
        fi
    fi

    echo ""
    info "Re-checking..."
    local -a still_missing=()
    for tool in go jq openssl node newman; do
        command -v "$tool" >/dev/null 2>&1 || still_missing+=("$tool")
    done

    if [[ ${#still_missing[@]} -eq 0 ]]; then
        success "All prerequisites installed successfully."
        return 0
    else
        warn "Still missing: ${still_missing[*]}. You may need to open a new shell (PATH changes) or install these manually."
        return 1
    fi
}

################################################################################
# WFM Group Selection Functions
################################################################################

# What a group is for, under its name in the group list.
print_group_description() {
    local desc="$1"
    [[ -z "$desc" || "$desc" == "No description" ]] && return 0
    echo "$desc" | fold -s -w 72 | sed 's/^/        /'
}

select_wfm_group() {
    local group_dir="$WFM_GROUP_DIR"
    
    if [[ ! -d "$group_dir" ]]; then
        warn "No groups directory found at: $group_dir" >&2
        return 1
    fi
    
    # Print to stderr so it displays to user (not captured by $() command substitution)
    {
        echo ""
        echo "📋 Available WFM Test Groups:"
        echo "================================================"
    } >&2
    
    # Collect available groups
    local groups=()
    local group_metadata=()
    
    for group_path in "$group_dir"/*; do
        if [[ -d "$group_path" && -f "$group_path/group.json" ]]; then
            local group_name=$(basename "$group_path")
            local group_json="$group_path/group.json"
            
            # Extract group info from group.json
            local version=$(jq -r '.version // "unknown"' "$group_json" 2>/dev/null || echo "unknown")
            local description=$(jq -r '.description // "No description"' "$group_json" 2>/dev/null || echo "No description")
            local test_count=$(jq '.testCases | length' "$group_json" 2>/dev/null || echo "0")
            
            groups+=("$group_path")
            group_metadata+=("$group_name|$version|$description|$test_count")
        fi
    done
    
    if [[ ${#groups[@]} -eq 0 ]]; then
        echo "❌ No test groups found. Create one with: bash ctt-creator/ctt-start.sh" >&2
        return 1
    fi
    
    # Display groups to stderr
    {
        for i in "${!groups[@]}"; do
            local metadata="${group_metadata[$i]}"
            IFS='|' read -r name version desc count <<< "$metadata"
            printf "  %2d) %s\n" "$((i+1))" "$name"
        done
        echo ""
    } >&2
    
    # Prompt for selection (read -p writes prompt to stderr by default)
    read -p "Select group (1-${#groups[@]}): " group_choice < /dev/tty
    
    if ! [[ "$group_choice" =~ ^[0-9]+$ ]] || [[ $group_choice -lt 1 || $group_choice -gt ${#groups[@]} ]]; then
        echo "❌ Invalid selection. Please enter a number between 1 and ${#groups[@]}" >&2
        return 1
    fi
    
    local selected_index=$((group_choice - 1))
    local selected_group="${groups[$selected_index]}"
    
    # Return group path to stdout (this will be captured by $())
    echo "$selected_group"
}

################################################################################
# Device Group Selection Function
################################################################################

select_device_group() {
    local group_dir="$DEVICE_GROUP_DIR"
    
    if [[ ! -d "$group_dir" ]]; then
        warn "No device groups directory found at: $group_dir" >&2
        return 1
    fi
    
    # Print to stderr so it displays to user (not captured by $() command substitution)
    {
        echo ""
        echo "📋 Available Device Test Groups:"
        echo "================================================"
    } >&2
    
    # Collect available groups
    local groups=()
    local group_metadata=()
    
    for group_path in "$group_dir"/*; do
        if [[ -d "$group_path" && -f "$group_path/group.json" ]]; then
            local group_name=$(basename "$group_path")
            local group_json="$group_path/group.json"
            
            # Extract group info from group.json
            local version=$(jq -r '.version // "unknown"' "$group_json" 2>/dev/null || echo "unknown")
            local description=$(jq -r '.description // "No description"' "$group_json" 2>/dev/null || echo "No description")
            local test_count=$(jq '.testCases | length' "$group_json" 2>/dev/null || echo "0")
            
            groups+=("$group_path")
            group_metadata+=("$group_name|$version|$description|$test_count")
        fi
    done
    
    if [[ ${#groups[@]} -eq 0 ]]; then
        echo "❌ No device test groups found. Create one with: bash ctt-creator/ctt-start.sh" >&2
        return 1
    fi
    
    # Display groups to stderr
    {
        for i in "${!groups[@]}"; do
            local metadata="${group_metadata[$i]}"
            IFS='|' read -r name version desc count <<< "$metadata"
            printf "  %2d) %s\n" "$((i+1))" "$name"
        done
        echo ""
    } >&2
    
    # Prompt for selection (read -p writes prompt to stderr by default)
    read -p "Select group (1-${#groups[@]}): " group_choice < /dev/tty
    
    if ! [[ "$group_choice" =~ ^[0-9]+$ ]] || [[ $group_choice -lt 1 || $group_choice -gt ${#groups[@]} ]]; then
        echo "❌ Invalid selection. Please enter a number between 1 and ${#groups[@]}" >&2
        return 1
    fi
    
    local selected_index=$((group_choice - 1))
    local selected_group="${groups[$selected_index]}"
    
    # Return group path to stdout (this will be captured by $())
    echo "$selected_group"
}

################################################################################
# WFM Supplier Test Execution (with Group Support)
################################################################################

execute_wfm_tests_with_url() {
    local wfm_url="${1:-}"
    
    
    # If WFM URL not provided, prompt user
    if [[ -z "$wfm_url" ]]; then
        echo ""
        read -p "Enter WFM URL: " wfm_url < /dev/tty
        [[ -z "$wfm_url" ]] && error "The WFM URL is required"
    fi

    log "🚀 Starting WFM Supplier Test Execution"
    log "   WFM Server: $wfm_url"
    
    if run_wfm_newman "$wfm_url"; then
        success "WFM Tests Completed"
    else
        error "WFM test execution failed"
    fi
}

execute_wfm_tests() {
    local wfm_url="${1:-}"
    local group_path="${2:-}"
    
    # If group path is provided, run with group filtering
    if [[ -n "$group_path" && -d "$group_path" ]]; then
        execute_wfm_tests_with_group "$wfm_url" "$group_path"
        return $?
    fi
    
    # Otherwise run without group filtering (legacy behavior)
    execute_wfm_tests_with_url "$wfm_url"
}

resolve_group_path() {
    local group_dir="$1"
    local group_ref="$2"

    if [[ -d "$group_ref" ]]; then
        echo "$group_ref"
        return 0
    fi

    if [[ -d "$group_dir/$group_ref" ]]; then
        echo "$group_dir/$group_ref"
        return 0
    fi

    return 1
}


get_group_json_files() {
    local group_path="$1"

    find "$group_path" -maxdepth 1 -type f -name '*.json' \
        ! -name 'group.json' \
        ! -name '.*.json' \
        -print | sort
}

json_file_is_valid() {
    local json_file="$1"

    jq empty "$json_file" >/dev/null 2>&1
}


json_file_is_postman_collection() {
    local json_file="$1"

    # Accept any Postman-like collection: must have item[] and either:
    #   - standard info object (Postman v2.1, portman generated, etc.)
    #   - portman-style _ object with postman_id
    jq -e '
        (.item? | type == "array") and
        (
            (.info? | type == "object") or
            (._? | type == "object" and has("postman_id"))
        )
    ' "$json_file" >/dev/null 2>&1
}

postman_item_count() {
    local collection_file="$1"

    jq '[.. | objects | select(has("request"))] | length' "$collection_file" 2>/dev/null || echo "0"
}

filter_postman_collection_by_group() {
    local collection_file="$1"
    local group_json="$2"
    local output_file="$3"

    jq --slurpfile group "$group_json" '
        ($group[0].testCases // []) as $ids
        | def normalized_name:
            (.name? // "" | gsub(" "; "_") | ascii_downcase);
        def matches_group_id:
            ((.id? as $id | $ids | index($id)) != null)
            or ((.name? as $name | $ids | index($name)) != null)
            or ((normalized_name as $name | $ids | index($name)) != null);
        def prune_items:
            if type == "object" and (.item? | type == "array") then
                .item = [
                    .item[]
                    | prune_items
                    | select(matches_group_id or ((.item? // []) | length > 0))
                ]
            else
                .
            end;
        prune_items
    ' "$collection_file" > "$output_file"
}

discover_wfm_group_collections() {
    local group_path="$1"
    local group_json="$2"
    local collections=()

    echo "[DEBUG] Reading group.json: $group_json" >&2

    local testcase_paths=()
    mapfile -t testcase_paths < <(
        jq -r '.FolderPath[]? // empty' "$group_json" 2>/dev/null
    )

    if [[ ${#testcase_paths[@]} -eq 0 ]]; then
        echo "[WARN] FolderPath not defined in group.json" >&2
        return
    fi

    for testcases_path in "${testcase_paths[@]}"; do
        [[ "$testcases_path" != /* ]] && testcases_path="$CONFORMANCE_DIR/$testcases_path"

        echo "[DEBUG] Looking inside: $testcases_path" >&2

        if [[ ! -d "$testcases_path" ]]; then
            echo "[WARN] Testcases folder not found: $testcases_path" >&2
            continue
        fi

        shopt -s nullglob
        local files=("$testcases_path"/*.json)
        shopt -u nullglob

        if [[ ${#files[@]} -eq 0 ]]; then
            echo "[WARN] No JSON files found in: $testcases_path" >&2
            continue
        fi

        echo "[DEBUG] Found ${#files[@]} JSON files" >&2

        for json_file in "${files[@]}"; do
            echo "[DEBUG] Checking: $(basename "$json_file")" >&2

            if ! jq empty "$json_file" >/dev/null 2>&1; then
                echo "[WARN] Invalid JSON: $(basename "$json_file")" >&2
                continue
            fi

            echo "[DEBUG] Valid JSON" >&2

            if jq -e '
                (.item? | type == "array") and
                (
                    (.info? | type == "object") or
                    (._? | type == "object" and has("postman_id"))
                )
            ' "$json_file" >/dev/null 2>&1; then

                echo "[DEBUG] ✅ Selected as Postman collection" >&2
                collections+=("$json_file")
            else
                echo "[DEBUG] ❌ Not a Postman collection" >&2
            fi
        done
    done

    echo "[DEBUG] Total selected collections: ${#collections[@]}" >&2

    if [[ ${#collections[@]} -gt 0 ]]; then
        printf '%s\n' "${collections[@]}"
    fi
}
discover_group_scenario_files() {
    local group_path="$1"
    local scenario_files=()

    local group_json="$group_path/group.json"

    echo "[DEBUG] Reading group.json: $group_json" >&2

    local testcase_paths=()
    mapfile -t testcase_paths < <(
        jq -r '.FolderPath[]? // empty' "$group_json" 2>/dev/null
    )

    if [[ ${#testcase_paths[@]} -eq 0 ]]; then
        warn "FolderPath not defined in group.json"
        return
    fi

    for testcases_path in "${testcase_paths[@]}"; do
        [[ "$testcases_path" != /* ]] && testcases_path="$CONFORMANCE_DIR/$testcases_path"

        echo "[DEBUG] Looking for scenario files in: $testcases_path" >&2

        if [[ ! -d "$testcases_path" ]]; then
            warn "Testcases folder not found: $testcases_path"
            continue
        fi

        shopt -s nullglob
        local files=("$testcases_path"/*.json)
        shopt -u nullglob

        if [[ ${#files[@]} -eq 0 ]]; then
            warn "No JSON files found in: $testcases_path"
            continue
        fi

        echo "[DEBUG] Found ${#files[@]} JSON file(s)" >&2

        for json_file in "${files[@]}"; do
            echo "[DEBUG] Checking: $(basename "$json_file")" >&2

            if ! json_file_is_valid "$json_file"; then
                warn "Skipping invalid JSON file: $(basename "$json_file")"
                continue
            fi

            if jq -e '
                type == "array" and
                any(.[]?; type == "object" and (.steps? | type == "array"))
            ' "$json_file" >/dev/null 2>&1; then
                echo "[DEBUG] ✅ Scenario file selected: $(basename "$json_file")" >&2
                scenario_files+=("$json_file")
            else
                echo "[DEBUG] ❌ Not a scenario file: $(basename "$json_file")" >&2
            fi
        done
    done

    echo "[DEBUG] Total scenario files selected: ${#scenario_files[@]}" >&2

    if [[ ${#scenario_files[@]} -gt 0 ]]; then
        printf '%s\n' "${scenario_files[@]}"
    fi
}

build_device_group_scenarios() {
    local group_path="$1"
    local output_file="$2"
    local group_json="$group_path/group.json"
    local scenario_files=()

    mapfile -t scenario_files < <(discover_group_scenario_files "$group_path")

    if [[ ${#scenario_files[@]} -eq 0 ]]; then
        error "No scenario JSON files found in group: $group_path"
    fi

    jq -s --slurpfile group "$group_json" '
        ($group[0].testCases // []) as $ids
        | [ .[] | select(type == "array") | .[] | select(type == "object") ] as $all
        | (
            if ($ids | length) == 0 then
                # No filter: run every scenario with all its steps
                $all | map(.steps = (.steps // []))
            else
                # A scenario runs with the steps the group lists by ID. If the
                # group lists only the scenario ID, the scenario runs whole.
                [
                    $all[]
                    | ([ .steps[]? | select(.id? as $id | $ids | index($id) != null) ]) as $listed
                    | if ($listed | length) > 0 then .steps = $listed
                      elif ((.id? as $id | $ids | index($id)) != null) then .steps = (.steps // [])
                      else empty
                      end
                ]
            end
          ) as $filtered
        # If the ID-based filter matched nothing (testCases IDs are UUIDs from a
        # Postman collection, scenario IDs are string slugs), fall back to running
        # all scenarios with all their steps — preserves behaviour for groups like
        # diamond that carry both Postman and scenario files.
        | if ($filtered | length) > 0 then $filtered
          else $all | map(.steps = (.steps // []))
          end
        | unique_by(.id // .name // tostring)
    ' "${scenario_files[@]}" > "$output_file"

    local scenario_count
    scenario_count=$(jq 'length' "$output_file")

    if [[ "$scenario_count" -eq 0 ]]; then
        error "No scenarios in group files matched test IDs from: $group_json"
    fi

    info "Matched $scenario_count scenario(s) from ${#scenario_files[@]} group file(s)" >&2
}

create_temp_scenarios_file() {
    mktemp /tmp/margo-device-scenarios.XXXXXX.json
}

# Every run ends here: the HTML report is turned into a signed PDF next to it
# (ctt-runner/reports/scripts/pdf_ops.sh). The first time, that script installs its PDF tools
# (sudo + internet, about a minute).
#   CTT_SIGN_REPORT=no       skip the signed PDF
#   CTT_SIGN_CERT, CTT_SIGN_PASSPHRASE
#                            sign with your own .p12 instead of the CTT's
#                            self-signed CA (kept in ctt-runner/reports/scripts/signing-identity/)
# A failure here never changes the test result; the HTML report stays as it is.
sign_report() {
    local html="$1"
    [[ -f "$html" ]] || return 0
    local signer="$SCRIPT_DIR/reports/scripts/pdf_ops.sh"
    if [[ ! -f "$signer" ]]; then
        warn "Signed PDF not created: $signer not found"
        return 0
    fi
    if [[ "${CTT_SIGN_REPORT:-yes}" =~ ^[nN] ]]; then
        info "Signed PDF skipped (CTT_SIGN_REPORT=${CTT_SIGN_REPORT})."
        return 0
    fi

    local out_dir base
    out_dir="$(dirname "$html")"
    base="$(basename "$html" .html)"
    local args=("$html")
    [[ -n "${CTT_SIGN_CERT:-}" ]] && args+=("$CTT_SIGN_CERT" "${CTT_SIGN_PASSPHRASE:-}")

    log "🔏 Creating the signed PDF report (the first time this installs the PDF tools and may ask for sudo)..."
    if PDF_OUTPUT_DIR="$out_dir" bash "$signer" "${args[@]}" > "$out_dir/report-signing.log" 2>&1 \
        && [[ -f "$out_dir/${base}_signed.pdf" ]]; then
        rm -f "$out_dir/${base}.pdf"
        success "Signed report: $out_dir/${base}_signed.pdf"
        if [[ -z "${CTT_SIGN_CERT:-}" ]]; then
            info "Signer CA certificate (send it with the report): $out_dir/ca_cert.pem"
        fi
    else
        warn "Signed PDF not created (the HTML report is not affected). Last lines of $out_dir/report-signing.log:"
        tail -n 6 "$out_dir/report-signing.log" 2>/dev/null | sed 's/^/      /' >&2
        warn "After fixing the cause, sign this report with:"
        echo "      PDF_OUTPUT_DIR=$out_dir bash $signer $html" >&2
    fi
    return 0
}

get_ctt_margo_version() {
    echo "1.0.0-rc.3"
}

# Validate MIAF cert fixtures before running scenarios.
# Catches cert/key mismatches and wrong CA chains early with a clear message.
validate_miaf_certs() {
    local cert_dir="$1"
    local errors=0

    local svid_cert="$cert_dir/svid-cert.pem"
    local svid_key="$cert_dir/svid-key.pem"
    local svid_ca="$cert_dir/svid-ca.pem"

    # All three files must exist
    for f in "$svid_cert" "$svid_key" "$svid_ca"; do
        if [[ ! -f "$f" ]]; then
            error "MIAF cert missing: $f"
        fi
    done

    # SVID cert and key must be a matching pair
    local cert_pub key_pub
    cert_pub=$(openssl x509 -in "$svid_cert" -noout -pubkey 2>/dev/null | openssl md5 2>/dev/null)
    key_pub=$(openssl ec  -in "$svid_key"  -pubout 2>/dev/null | openssl md5 2>/dev/null)
    if [[ -z "$cert_pub" || "$cert_pub" != "$key_pub" ]]; then
        log "❌ SVID cert/key mismatch: $svid_cert and $svid_key are not a matching pair."
        log "   Re-run mis.sh (or ctt-mis.sh) to regenerate and re-copy the SVID certs."
        errors=$((errors + 1))
    fi

    # Trust bundle CA must verify the SVID cert
    if ! openssl verify -CAfile "$svid_ca" "$svid_cert" >/dev/null 2>&1; then
        log "❌ Trust bundle CA does not verify the SVID cert."
        log "   $svid_ca did not sign $svid_cert."
        log "   Ensure both files come from the same MIS issuance."
        errors=$((errors + 1))
    fi

    if [[ $errors -gt 0 ]]; then
        error "MIAF cert validation failed ($errors error(s)). Fix certs before running scenarios."
    fi

    log "✅ MIAF cert validation passed (cert/key match, CA chain OK)"
}

confirm_version_mismatch() {
    local claimed_app_version="$1"
    local ctt_margo_version
    ctt_margo_version=$(get_ctt_margo_version)

    if [[ -n "$claimed_app_version" && "$claimed_app_version" != "unknown" && "$claimed_app_version" != "$ctt_margo_version" ]]; then
        echo ""
        echo -e "\033[32m⚠ Version Mismatch: Claimed App Version ($claimed_app_version) differs from CTT Margo Version ($ctt_margo_version)\033[0m"
        read -p "Do you want to continue? (y/N): " confirm_continue < /dev/tty
        [[ "${confirm_continue,,}" == "y" ]] || error "Aborted due to version mismatch"
    fi
}

# Copy the identity chosen in "Setup MIAF Identity" into the folder the runner
# reads, and check it. Every WFM run needs it: all rc.3 requests go over mTLS.
wfm_install_run_identity() {
    local cert_dir="$1"
    local miaf_dir="$CONFORMANCE_DIR/wfm-supplier/utils/fixtures/miaf"
    local real_dir="$miaf_dir/real"
    # Prefer real/ certs (Symphony SVID, signed by the real trust-bundle CA).
    # Fall back to the test-CA certs in miaf/ when real/ is absent (mock-server runs).
    rm -f "$cert_dir/svid-previous-cert.pem" "$cert_dir/svid-previous-key.pem"
    if [[ -f "$real_dir/client-svid-cert.pem" && -f "$real_dir/client-svid-key.pem" ]]; then
        cp "$real_dir/client-svid-cert.pem" "$cert_dir/svid-cert.pem"
        cp "$real_dir/client-svid-key.pem"  "$cert_dir/svid-key.pem"
        [[ -f "$real_dir/trust-bundle-ca.pem" ]] && cp "$real_dir/trust-bundle-ca.pem" "$cert_dir/svid-ca.pem"
        # The SVID in use before the last rotation, kept by Setup MIAF Identity.
        if [[ -f "$real_dir/previous-client-svid-cert.pem" && -f "$real_dir/previous-client-svid-key.pem" ]]; then
            cp "$real_dir/previous-client-svid-cert.pem" "$cert_dir/svid-previous-cert.pem"
            cp "$real_dir/previous-client-svid-key.pem"  "$cert_dir/svid-previous-key.pem"
        fi
        log "Using real SVID certs (Symphony trust bundle)"
    elif [[ -f "$miaf_dir/client-svid-cert.pem" && -f "$miaf_dir/client-svid-key.pem" ]]; then
        cp "$miaf_dir/client-svid-cert.pem" "$cert_dir/svid-cert.pem"
        cp "$miaf_dir/client-svid-key.pem"  "$cert_dir/svid-key.pem"
        log "Using test SVID certs (mock server trust bundle)"
    else
        error "SVID certs not found at $miaf_dir — required for mtls:true steps"
    fi

    validate_miaf_certs "$cert_dir"
}

run_wfm_scenario_group() {
    local wfm_url="$1"
    local group_path="$2"
    local group_name="$3"
    local miaf_url="${4:-$wfm_url}"
    local scenario_file
    local report_file
    local scenario_runner="$CONFORMANCE_DIR/wfm-supplier/scripts/run_wfm_scenarios.js"
    local cert_dir="$CONFORMANCE_DIR/wfm-supplier/utils/certs"
    local claimed_app_version
    claimed_app_version=$(jq -r '.version // ""' "$group_path/group.json" 2>/dev/null)

    command -v node >/dev/null 2>&1 || error "Node.js not found. Install Node.js before running WFM scenario tests."
    [[ -f "$scenario_runner" ]] || error "WFM scenario runner not found: $scenario_runner"
    
    # Generate fresh device certificate for each run to avoid 409 Conflict
    log "Generating fresh device certificate for test run..."
    local temp_device_id="device-$(date +%s)"
    mkdir -p "$cert_dir"
    openssl ecparam -name prime256v1 -genkey -noout -out "$cert_dir/device.key" >/dev/null 2>&1
    openssl req -new -x509 -days 365 \
        -key "$cert_dir/device.key" \
        -out "$cert_dir/device-cert.pem" \
        -subj "/C=IN/ST=GGN/L=Sector48/O=Margo/OU=Conformance/CN=$temp_device_id" >/dev/null 2>&1

    [[ -f "$cert_dir/device.key" ]] || error "Device private key not found: $cert_dir/device.key"
    [[ -f "$cert_dir/device-cert.pem" ]] || error "Device certificate not found: $cert_dir/device-cert.pem"

    wfm_install_run_identity "$cert_dir"

    confirm_version_mismatch "$claimed_app_version"

    # ── Deployment scenario: a deployment must be assigned to the CTT's client ──
    # Assigning a deployment is an operator action in the WFM (its console or
    # its own northbound API). That is outside the Margo specification, so the
    # CTT does not do it: it says what is needed and the scenario's poll step
    # waits for the deployment to appear on the Management Interface.
    if jq -r '.testCases[]?' "$group_path/group.json" 2>/dev/null \
           | grep -q "^wfm-multi-component-deployment$"; then
        local client_spiffe_id=""
        if [[ -f "$cert_dir/svid-cert.pem" ]]; then
            client_spiffe_id=$(openssl x509 -in "$cert_dir/svid-cert.pem" -text -noout 2>/dev/null \
                | grep -oE 'URI:spiffe://[^[:space:]]+' | head -1 | sed 's/URI://')
        fi
        echo ""
        echo "────────────────────────────────────────────────────────────────"
        info "This group tests deployment delivery. The WFM must have an application"
        info "deployment assigned to the CTT's client:"
        info "    ${client_spiffe_id:-the client SPIFFE ID shown by Setup MIAF Identity}"
        info "with two components, one 'wait: true' and one 'wait: false'."
        info "Assign it in your WFM (console or operator API). If it is not there yet,"
        info "the run waits up to 5 minutes for it when it reaches that scenario, shows"
        info "how to assign it, and reports those steps NOT RUN if none appears."
        info "Sandbox Symphony: run this once before the run (or now, in a second terminal):"
        info "    bash $CONFORMANCE_DIR/wfm-supplier/utils/fixtures/provision-multi-component.sh"
        echo "────────────────────────────────────────────────────────────────"
        echo ""
    fi

    scenario_file=$(create_temp_scenarios_file)
    build_device_group_scenarios "$group_path" "$scenario_file"

    report_file="$RUNNER_WFM/wfm-scenario-report-${group_name}_$(date +%Y%m%d_%H%M%S).html"

    log "▶️  Running WFM scenarios from group: $group_name"
    log "📊 Report: $report_file"

    # Repair ownership on paths that sudo-based setup (wfm.sh, cert copies) may
    # have left owned by root — the node runner and ORAS both run as this user.
    local current_user
    current_user=$(id -un)
    sudo chown -R "${current_user}:${current_user}" "$cert_dir" 2>/dev/null || true
    sudo chown -R "${current_user}:${current_user}" "${HOME}/.docker" 2>/dev/null || true

    set +e
    local miaf_flags=()
    [[ "$miaf_url" != "$wfm_url" ]] && miaf_flags=("--miaf-url" "$miaf_url")
    node "$scenario_runner" "$wfm_url" "$scenario_file" "$report_file" "$cert_dir" "$group_name" "$claimed_app_version" "${miaf_flags[@]}"
    local result=$?
    set -e
    sign_report "$report_file"

    rm -f "$scenario_file"

    if [[ $result -eq 0 ]]; then
        success "WFM scenario tests completed for group: $group_name"
    else
        error "WFM scenario tests failed for group: $group_name"
    fi
}

# Run a Postman collection generated from the OpenAPI file (ctt-creator →
# "OpenAPI spec based contract tests") with the CTT runner: each documented
# response of each endpoint becomes one check, sent over mTLS.
run_wfm_contract_collection() {
    local wfm_url="$1"
    local collection_file="$2"
    local scenario_runner="$CONFORMANCE_DIR/wfm-supplier/scripts/run_wfm_scenarios.js"
    local cert_dir="$CONFORMANCE_DIR/wfm-supplier/utils/certs"

    command -v node >/dev/null 2>&1 || error "Node.js not found. Install Node.js before running WFM tests."
    json_file_is_postman_collection "$collection_file" || error "Not a Postman collection: $collection_file"

    mkdir -p "$cert_dir"
    openssl ecparam -name prime256v1 -genkey -noout -out "$cert_dir/device.key" >/dev/null 2>&1
    openssl req -new -x509 -days 365 \
        -key "$cert_dir/device.key" \
        -out "$cert_dir/device-cert.pem" \
        -subj "/C=IN/ST=GGN/L=Sector48/O=Margo/OU=Conformance/CN=device-$(date +%s)" >/dev/null 2>&1

    wfm_install_run_identity "$cert_dir"

    # Unregistering a device is left out unless asked for, as in the core group.
    local run_collection="$collection_file"
    local filtered_collection=""
    if jq -e '[.. | objects | select(has("request")) | select(.request.method == "DELETE")] | length > 0' "$collection_file" >/dev/null 2>&1; then
        echo ""
        echo "  This collection includes DELETE /api/v1/capabilities/{deviceId} (unregister a device)."
        echo "  The sandbox Symphony WFM stops when it receives this request — include it only for your own WFM."
        local include_delete
        read -p "  Include the unregister request? [y/N]: " include_delete < /dev/tty
        if [[ "${include_delete,,}" != "y" ]]; then
            filtered_collection=$(mktemp /tmp/margo-wfm-contract.XXXXXX.json)
            jq 'def drop_delete: if type == "object" and (.item? | type == "array")
                    then .item = [.item[] | select((.request?.method? // "") != "DELETE") | drop_delete] else . end;
                drop_delete' "$collection_file" > "$filtered_collection"
            run_collection="$filtered_collection"
        fi
    fi

    mkdir -p "$RUNNER_WFM"
    local report_file="$RUNNER_WFM/wfm-contract-report_$(date +%Y%m%d_%H%M%S).html"
    log "▶️  Running contract tests against: $wfm_url"
    log "📊 Report: $report_file"

    set +e
    node "$scenario_runner" "$wfm_url" "$run_collection" "$report_file" "$cert_dir" "contract" "$(get_ctt_margo_version)"
    local result=$?
    set -e
    sign_report "$report_file"
    [[ -n "$filtered_collection" ]] && rm -f "$filtered_collection"

    if [[ $result -eq 0 ]]; then
        success "WFM contract tests completed"
    else
        warn "WFM contract tests finished with failures — see the report"
    fi
}

run_wfm_newman() {
    local wfm_url="${1:-}"
    local collection_file="${2:-$CONFORMANCE_DIR/wfm-supplier/postman_collection.json}"
    local report_prefix="${3:-wfm-test-report}"
    local wfm_supplier_dir="$CONFORMANCE_DIR/wfm-supplier"
    local data_dir="$wfm_supplier_dir/newman-data"
    local env_file="$data_dir/device-agent.env.json"
    local iteration_file="$data_dir/device-agent.iteration.json"
    local cert_dir="$data_dir/certs"
    local local_ca_cert_file="$wfm_supplier_dir/certs/ca-cert.pem"
    local runtime_ca_cert_file="$cert_dir/ca-cert.pem"
    local runtime_collection="$wfm_supplier_dir/.collection.runtime.json"
    local report_file="${report_prefix}_$(date +%Y%m%d_%H%M%S).html"

    if [[ -z "$wfm_url" ]]; then
        if [[ -f "$env_file" ]]; then
            wfm_url=$(jq -r '.values[] | select(.key=="baseUrl") | .value' "$env_file" 2>/dev/null || echo "")
        fi
    fi

    [[ -z "$wfm_url" ]] && error "WFM URL not provided"
    [[ -d "$wfm_supplier_dir" ]] || error "WFM Supplier directory not found: $wfm_supplier_dir"
    [[ -f "$collection_file" ]] || error "Postman collection not found: $collection_file"
    [[ -f "$env_file" ]] || error "Newman environment not found: $env_file"

    command -v jq >/dev/null 2>&1 || error "jq not found. Install jq before running WFM tests."
    command -v newman >/dev/null 2>&1 || error "Newman not found. Install with: npm install -g newman newman-reporter-htmlextra"

    mkdir -p "$cert_dir"
    if [[ -f "$local_ca_cert_file" ]]; then
        cp "$local_ca_cert_file" "$runtime_ca_cert_file"
    elif [[ ! -f "$runtime_ca_cert_file" ]]; then
        error "Missing WFM CA certificate. Copy it to: $local_ca_cert_file"
    fi

    wfm_url="${wfm_url//v1aplha2/v1alpha2}"
    jq --arg baseUrl "$wfm_url" \
        '.values |= map(if .key == "baseUrl" then .value = $baseUrl else . end)' \
        "$env_file" > "$env_file.tmp"
    mv "$env_file.tmp" "$env_file"
    echo '[]' > "$iteration_file"

    cp "$collection_file" "$runtime_collection"
    
    # Use external jq filter file to avoid shell quoting issues
    local jq_filter_file="$wfm_supplier_dir/prepare_conformance_collection.jq"
    if [[ ! -f "$jq_filter_file" ]]; then
        error "JQ filter file not found: $jq_filter_file"
    fi
    
    jq -f "$jq_filter_file" "$runtime_collection" > "$runtime_collection.tmp"
    mv "$runtime_collection.tmp" "$runtime_collection"

    log "▶️  Running Newman against: $wfm_url"
    set +e
    (cd "$wfm_supplier_dir" && newman run "$runtime_collection" \
        --environment "$env_file" \
        --ssl-extra-ca-certs "$runtime_ca_cert_file" \
        --insecure \
        -r cli,htmlextra \
        --reporter-htmlextra-export "$report_file")
    local result=$?
    set -e

    rm -f "$runtime_collection" "$runtime_collection.tmp"

    if [[ -f "$wfm_supplier_dir/$report_file" ]]; then
        cp "$wfm_supplier_dir/$report_file" "$RUNNER_WFM/"
        success "Report: $RUNNER_WFM/$report_file"
    fi

    return $result
}

execute_wfm_tests_with_group() {
    local wfm_url="${1:-}"
    local group_path="${2:-}"
    local miaf_url="${3:-}"

    if [[ ! -d "$group_path" ]]; then
        error "Group path not found: $group_path"
    fi

    local group_json="$group_path/group.json"
    local group_name=$(basename "$group_path")

    if [[ ! -f "$group_json" ]]; then
        error "group.json not found in: $group_path"
    fi

    # If WFM URL not provided, prompt user
    if [[ -z "$wfm_url" ]]; then
        echo ""
        read -p "Enter WFM URL: " wfm_url < /dev/tty
        [[ -z "$wfm_url" ]] && error "The WFM URL is required"
        miaf_url="${miaf_url:-$wfm_url}"
    else
        miaf_url="${miaf_url:-$wfm_url}"
    fi

    log "🚀 Starting WFM Supplier Test Execution (Group Mode)"
    log "   Group: $group_name"
    log "   WFM Server: $wfm_url"
    [[ "$miaf_url" != "$wfm_url" ]] && log "   MIAF Server: $miaf_url"
    
    # Get group metadata
    local group_version=$(jq -r '.version // "unknown"' "$group_json")
    local group_desc=$(jq -r '.description // ""' "$group_json")
    local test_count=$(jq '.testCases | length' "$group_json")
    
    info "Group Details:"
    info "  Name: $group_name"
    info "  Version: $group_version"
    info "  Description: $group_desc"
    info "  Test cases: $test_count"

    # --- Scenario-format groups (primary path) -----------------------------------
    # Groups that store test data as a plain JSON array of scenario objects with
    # steps run through run_wfm_scenario_group.  These are identified by content,
    # not by filename.
    local group_scenario_files=()
    mapfile -t group_scenario_files < <(discover_group_scenario_files "$group_path")
    if [[ ${#group_scenario_files[@]} -gt 0 ]]; then
        log "📋 Found ${#group_scenario_files[@]} scenario file(s)"
        run_wfm_scenario_group "$wfm_url" "$group_path" "$group_name" "$miaf_url"
        return 0
    fi

    # --- Postman-collection groups (fallback path) ---------------------------
    # Discover every Postman collection in the group dir (plus any referenced via
    # collectionFiles in group.json) purely by content — file names are irrelevant.
    local group_collections=()
    mapfile -t group_collections < <(discover_wfm_group_collections "$group_path" "$group_json")

    if [[ ${#group_collections[@]} -eq 0 ]]; then
        error "No test data found for group: $group_name (place any *.json Postman collection in the group directory or list it under \"collectionFiles\" in group.json)"
    fi

    log "📋 Found ${#group_collections[@]} Postman collection file(s)"

    # Auto-sync: extract the UUID of every request in every collection file and append
    # any new ones to group.json testCases so the user never has to maintain IDs manually.
    local merged_ids
    merged_ids=$(
        # Existing IDs from group.json (preserve order)
        jq -r '.testCases[]?' "$group_json" 2>/dev/null
        # Request UUIDs found in each collection file (in file order)
        for cfile in "${group_collections[@]}"; do
            jq -r '.. | objects | select(has("request")) | .id? // empty | strings
                | select(test("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"))
            ' "$cfile" 2>/dev/null
        done
    )
    # Build deduplicated list preserving first-seen order
    local unique_ids
    unique_ids=$(echo "$merged_ids" | awk '!seen[$0]++')

    if [[ -n "$unique_ids" ]]; then
        local id_json
        id_json=$(echo "$unique_ids" | jq -R . | jq -s .)
        local current_count new_count
        current_count=$(jq '.testCases | length' "$group_json")
        jq --argjson ids "$id_json" '.testCases = $ids' "$group_json" > "$group_json.tmp" \
            && mv "$group_json.tmp" "$group_json"
        new_count=$(jq '.testCases | length' "$group_json")
        [[ "$new_count" -gt "$current_count" ]] && \
            info "Synced $new_count test IDs to group.json (was $current_count)"
    fi

    local scenario_runner="$CONFORMANCE_DIR/wfm-supplier/scripts/run_wfm_scenarios.js"
    command -v node >/dev/null 2>&1 || error "Node.js not found. Install Node.js before running WFM tests."
    [[ -f "$scenario_runner" ]] || error "WFM scenario runner not found: $scenario_runner"

    local cert_dir="$CONFORMANCE_DIR/wfm-supplier/utils/certs"
    local temp_device_id="device-$(date +%s)"
    mkdir -p "$cert_dir"
    openssl ecparam -name prime256v1 -genkey -noout -out "$cert_dir/device.key" >/dev/null 2>&1
    openssl req -new -x509 -days 365 \
        -key "$cert_dir/device.key" \
        -out "$cert_dir/device-cert.pem" \
        -subj "/C=IN/ST=GGN/L=Sector48/O=Margo/OU=Conformance/CN=$temp_device_id" >/dev/null 2>&1

    wfm_install_run_identity "$cert_dir"

    confirm_version_mismatch "$group_version"

    log "▶️  Running test cases from group: $group_name"
    echo ""

    local collection_index=0
    local executed_collections=0
    for group_collection in "${group_collections[@]}"; do
        collection_index=$((collection_index + 1))

        local filtered_collection="$group_path/.collection.${group_name}.${collection_index}.filtered.json"
        filter_postman_collection_by_group "$group_collection" "$group_json" "$filtered_collection"

        local matched_items
        matched_items=$(postman_item_count "$filtered_collection")
        if [[ "$matched_items" -eq 0 ]]; then
            warn "Skipping $(basename "$group_collection"): no runnable Postman items matched group.json"
            rm -f "$filtered_collection"
            continue
        fi

        log "   Collection: $(basename "$group_collection")"
        log "   Matched items: $matched_items"

        local report_suffix="${group_name}"
        [[ ${#group_collections[@]} -gt 1 ]] && report_suffix="${group_name}-${collection_index}"
        local report_file="$RUNNER_WFM/wfm-scenario-report-${report_suffix}_$(date +%Y%m%d_%H%M%S).html"
        log "📊 Report: $(basename "$report_file")"

        local current_user
        current_user=$(id -un)
        sudo chown -R "${current_user}:${current_user}" "$cert_dir" 2>/dev/null || true
        sudo chown -R "${current_user}:${current_user}" "${HOME}/.docker" 2>/dev/null || true

        set +e
        local miaf_flags=()
        [[ "$miaf_url" != "$wfm_url" ]] && miaf_flags=("--miaf-url" "$miaf_url")
        node "$scenario_runner" "$wfm_url" "$filtered_collection" "$report_file" "$cert_dir" "$group_name" "$group_version" "${miaf_flags[@]}"
        local result=$?
        set -e
        sign_report "$report_file"

        rm -f "$filtered_collection"

        if [[ $result -eq 0 ]]; then
            executed_collections=$((executed_collections + 1))
            success "WFM collection completed: $(basename "$group_collection")"
        else
            error "WFM test execution failed for group: $group_name"
        fi
    done

    if [[ "$executed_collections" -eq 0 ]]; then
        error "No runnable Postman items in $group_name matched test IDs from group.json"
    fi

    success "WFM Tests Completed for group: $group_name"
}

################################################################################
# Device Test Scenarios Selection
################################################################################

show_device_test_scenarios_menu() {
    echo "" >&2
    echo "Which test scenarios would you like to run?" >&2
    echo "1. Group-based test scenarios (select from available groups)" >&2
    echo "" >&2
    echo "Q) Quit" >&2
    echo "" >&2
}

select_device_test_scenarios() {
    while true; do
        show_device_test_scenarios_menu
        # Print prompt to stderr so it does not get captured in command substitution
        echo -n "Select option (1 or Q): " >&2
        read choice
        
        case "${choice,,}" in
            1|group)
                local device_group
                device_group=$(select_device_group)
                if [[ -z "$device_group" ]]; then
                    error "No device group selected"
                fi
                
                local group_scenarios
                group_scenarios=$(create_temp_scenarios_file)
                build_device_group_scenarios "$device_group" "$group_scenarios"

                echo "$group_scenarios"
                return 0
                ;;
            q|quit)
                info "Exiting..."
                exit 0
                ;;
            *)
                error "Invalid option. Please select 1 or Q"
                ;;
        esac
    done
}

################################################################################
# Device Supplier Test Execution
################################################################################

execute_device_tests() {
    local test_scenarios="${1:-}"
    local group_path="${2:-}"

    if [[ -z "$test_scenarios" ]]; then
        error "Test scenarios file not provided"
    fi

    local claimed_app_version="unknown"
    [[ -n "$group_path" && -f "$group_path/group.json" ]] && \
        claimed_app_version=$(jq -r '.version // "unknown"' "$group_path/group.json" 2>/dev/null)
    local ctt_margo_version
    ctt_margo_version=$(get_ctt_margo_version)
    confirm_version_mismatch "$claimed_app_version"

    # Groups may opt into flexible-order mode (fixed_first onboarding, then the
    # rest of the scenarios in a random relative order) via a "flexibleOrder"
    # key in their group.json — same check used by the interactive device flow,
    # duplicated here because this function is also reached directly via
    # `../ctt-runner/ctt-start.sh device <group>`.
    local extra_flags=()
    [[ -n "$group_path" ]] && extra_flags+=("-group" "$(basename "$group_path")")
    if [[ -n "$group_path" && "$(jq -r '.flexibleOrder // false' "$group_path/group.json" 2>/dev/null)" == "true" ]]; then
        extra_flags+=("-flexible-order")
        info "🔀 Flexible-order mode enabled for group '$(basename "$group_path")'."
    fi
    
    log "🚀 Starting Device Supplier Test Execution"
    log "   (Mock server + test runner orchestration)"
    
    # Check if test scenarios file exists
    if [[ ! -f "$test_scenarios" ]]; then
        error "Test scenarios not found: $test_scenarios"
    fi
    
    log "📋 Test Scenarios: $(basename "$test_scenarios")"
    
    cd "$CONFORMANCE_DIR/device-supplier"

    if ! device_identity_ready "./certs"; then
        error "MIAF identity not found in $CONFORMANCE_DIR/device-supplier/certs. Run: ctt-start.sh → Device Supplier → Setup Identity"
    fi

    device_prepare_runtime

    # Copy test scenarios from Data-Generator or use custom scenarios
    log "📋 Staging test scenarios..."
    mkdir -p ./device-scenarios
    
    # Check if source and destination are the same (for custom scenarios)
    local resolved_source=$(cd "$(dirname "$test_scenarios")" && pwd -P)/$(basename "$test_scenarios")
    local resolved_dest=$(cd "$(dirname ./device-scenarios)" && pwd -P)/$(basename ./device-scenarios)/test-scenarios.json
    
    if [[ "$resolved_source" != "$resolved_dest" ]]; then
        cp "$test_scenarios" ./device-scenarios/test-scenarios.json
    fi
    
    # A group that observes a real device needs the mock WFM the device is
    # already connected to: it is used as it is, and left running afterwards.
    local observes_real_device=false
    if jq -e '[.[].steps[]? | select(has("observe"))] | length > 0' ./device-scenarios/test-scenarios.json >/dev/null 2>&1; then
        observes_real_device=true
        if [[ ! -f /tmp/wfm-server.pid ]] || ! kill -0 "$(cat /tmp/wfm-server.pid)" 2>/dev/null; then
            error "This group judges a real device. Start the mock WFM first (ctt-start.sh → Device Supplier → 2), point the device at it, then run the group again."
        fi
        log "Using the running Mock WFM Server (PID: $(cat /tmp/wfm-server.pid)) — the device under test must be connected to it."
    else
        device_stop_stale_server

        # Repair ownership on cert paths that sudo-based setup may have left as root
        local current_user
        current_user=$(id -un)
        sudo chown -R "${current_user}:${current_user}" "./certs" 2>/dev/null || true
        sudo chown -R "${current_user}:${current_user}" "${HOME}/.docker" 2>/dev/null || true

        log "🚀 Starting Mock WFM Server (background)..."
        device_launch_server "./certs"
        success "Mock WFM Server started (PID: $(cat /tmp/wfm-server.pid))"
    fi

    # Run tests against mock server
    log "▶️  Running Device Conformance Tests (as device agent)..."
    echo ""

    # The CTT and its mock WFM run on the same host
    local cli_host="localhost"
    extra_flags+=("-miaf-url" "https://${cli_host}:3003/v1alpha2/margo")

    local test_result=0
    if ./bin/run_tests -url "https://${cli_host}:3001/v1alpha2/margo" -claimed-app-version "$claimed_app_version" -ctt-margo-version "$ctt_margo_version" "${extra_flags[@]}" 2>&1 | tee "$RUNNER_DEVICE/test-execution.log"; then
        test_result=0
    else
        test_result=1
    fi
    
    # Stop mock server
    if [[ "$observes_real_device" == true ]]; then
        log "Mock WFM Server left running for the device under test."
    elif [[ -f /tmp/wfm-server.pid ]]; then
        log "⛔ Stopping Mock WFM Server..."
        local pid=$(cat /tmp/wfm-server.pid)
        if kill -0 $pid 2>/dev/null; then
            kill -15 $pid
            sleep 1
            success "Mock server stopped (PID: $pid)"
        fi
        rm -f /tmp/wfm-server.pid
    fi
    
    # Find and copy generated report
    local latest_report=$(find reports -name "conformance-report-*.html" -type f -printf '%T@ %p\n' 2>/dev/null | sort -rn | head -1 | cut -d' ' -f2-)
    
    if [[ -n "$latest_report" && -f "$latest_report" ]]; then
        cp "$latest_report" "$RUNNER_DEVICE/"
        success "Device Tests Completed"
        success "Report: $RUNNER_DEVICE/$(basename "$latest_report")"
        success "Execution log: $RUNNER_DEVICE/test-execution.log"
        sign_report "$RUNNER_DEVICE/$(basename "$latest_report")"
    else
        success "Device Tests Completed"
        info "Reports location: $CONFORMANCE_DIR/device-supplier/reports/"
    fi

    # The report is written also when tests fail; the failure is reported last.
    if [[ $test_result -ne 0 ]]; then
        error "Some device tests failed. See the report above and $RUNNER_DEVICE/test-execution.log"
    fi
}

################################################################################
# Persona Selection Menu
################################################################################

show_persona_menu() {
    echo ""
    echo "Which Margo Persona do you want to test?"
    echo "1. WFM Supplier"
    echo "2. Device Supplier"
    echo "3. Application Supplier"
    echo ""
    echo "P) Check/Install Prerequisites (go, jq, openssl, node, newman)"
    echo "H) Help"
    echo "Q) Quit"
    echo ""
}

select_application() {

    local app_dir="$APPLICATION_DIR"
    local apps=()

    {
        echo ""
        echo "Available Applications"
        echo "======================"
    } >&2

    for dir in "$app_dir"/*; do
        [[ -d "$dir" ]] && apps+=("$dir")
    done

    if [[ ${#apps[@]} -eq 0 ]]; then
        echo "No applications found in $app_dir" >&2
        return 1
    fi

    {
        for i in "${!apps[@]}"; do
            printf "  %d) %s\n" \
                "$((i+1))" \
                "$(basename "${apps[$i]}")"
        done
        echo "  o) Application Package in an Application Registry (OCI reference)"
        echo ""
    } >&2

    read -p "Select Application (1-${#apps[@]} or o): " choice < /dev/tty

    # An Application Package in a registry: the validator pulls it with ORAS
    # and also runs the Application Registry checks.
    if [[ "${choice,,}" == "o" ]]; then
        local package_ref
        read -p "OCI reference (e.g. oci://harbor.machine:8443/library/<app>:<metadata.version>): " package_ref < /dev/tty
        if [[ -z "$package_ref" ]]; then
            echo "No OCI reference given" >&2
            return 1
        fi
        echo "$package_ref"
        return 0
    fi

    if ! [[ "$choice" =~ ^[0-9]+$ ]] || \
       [[ $choice -lt 1 || $choice -gt ${#apps[@]} ]]; then
        echo "Invalid selection" >&2
        return 1
    fi

    echo "${apps[$((choice-1))]}"
}

run_application_supplier() {
    local selected_app

    selected_app=$(select_application)

    local app_name
    app_name=$(basename "$selected_app")

    log "Selected Application: $app_name"

    cd "$APPLICATION_SERVICE_DIR" || \
        error "Unable to enter Application Supplier Service"

    log "Running Application Validation..."

    go run . "$selected_app"

    local result=$?

    # if [[ $result -eq 0 ]]; then
    #     success "Application Validation Passed"
    # else
    #     error "Application Validation Failed"
    # fi
}

################################################################################
# Help Function
################################################################################

show_help() {
    cat << 'EOF'

╔═══════════════════════════════════════════════════════════════════════════╗
║            Conformance Test Runner - Help                                 ║
╚═══════════════════════════════════════════════════════════════════════════╝

DESCRIPTION:
  Runs a test group against a real system under test and writes an HTML
  report. Spec baseline: Margo 1.0.0-rc.3 — every Management Interface
  request is sent over mutual TLS with an X.509-SVID (MIAF).

USAGE:
  bash ctt-runner/ctt-start.sh                           # Interactive menu
  bash ctt-runner/ctt-start.sh wfm <GROUP> <WFM_URL>     # Run a WFM group
  bash ctt-runner/ctt-start.sh device <GROUP>            # Run a Device group
  bash ctt-runner/ctt-start.sh application               # Validate an Application Package
  bash ctt-runner/ctt-start.sh help                      # Show this help

PERSONAS:

  WFM Supplier        (you bring a WFM; the CTT acts as its device client)
    1. Setup MIAF Identity   — once: give the folder with the CTT's client SVID
    2. Functional tests      — pick a group, enter the WFM URL
    3. OpenAPI spec based contract tests — run the collection generated from
                               the OpenAPI file
    Guide: docs/wfm-supplier-setup-guide.md

  Device Supplier     (you bring a device; the CTT acts as its WFM)
    1. Setup Identity        — once: give the folder with the two SVIDs
    2. Start Mock WFM Server — Management Interface, mTLS on port 3003
    3. Run Tests             — pick a group; the CTT's device client runs it
    4. Stop Mock WFM Server
    A real device-agent is tested by pointing it at the mock WFM.
    Guide: docs/device-supplier-setup-guide.md

  Application Supplier
    Validates an Application Package folder under test-suites/app-supplier/.

TEST GROUPS:

  A group is a named list of test IDs with a spec version. The group list
  shows each group with its description. Start with quick-check to confirm
  the setup; run core for the conformance result.

    test-suites/<persona>/<group>/
    ├── group.json      name, version, description, test folder, test IDs
    └── test-cases/     scenario files (JSON) or a Postman collection (WFM)

  Groups are created with the other CLI: bash ctt-creator/ctt-start.sh

REPORTS:

  ctt-runner/reports/wfm-supplier/           WFM Supplier
  ctt-runner/reports/device-supplier/        Device Supplier
  ctt-runner/reports/application-supplier/   Application Supplier

  Each row shows the request, the expected and the actual result, and the
  requirement IDs the check is evidence for. A check that could not be run
  is reported as NOT RUN with the reason. Both personas use the same layout.

  Every run also writes the report as a signed PDF (<report>_signed.pdf) and
  the signer's CA certificate (ca_cert.pem) to the same folder. Check one with:
    ctt-runner/reports/scripts/.venv/bin/python ctt-runner/reports/scripts/pdf_verify.py <report>_signed.pdf ca_cert.pem
  CTT_SIGN_REPORT=no skips it; CTT_SIGN_CERT=<file.p12> (and
  CTT_SIGN_PASSPHRASE) signs with your own certificate.

REQUIREMENTS:

  jq, openssl                 both personas
  Node.js                     WFM Supplier
  Go 1.24+                    Device Supplier, Application Supplier
  oras                        WFM Supplier: Application Registry checks

TROUBLESHOOTING:

  See the Troubleshooting section of the setup guide of your persona.
  Mock WFM log (Device Supplier): /tmp/wfm-server.log

EOF
}

################################################################################
# WFM Certificate Info
################################################################################

show_wfm_cert_info() {
    cat << 'EOF'

╔═══════════════════════════════════════════════════════════════════════════╗
║            WFM Certificate Setup Required                                 ║
╚═══════════════════════════════════════════════════════════════════════════╝

BEFORE RUNNING WFM SUPPLIER TESTS:

  1. Copy WFM CA Certificate to Device Agent VM
  
     Copy FROM (WFM Server):
       ~/symphony/api/certificates/ca-cert.pem
     
     Copy TO (Device Agent VM - this machine):
       ~/sandbox/conformance/wfm-supplier/certs/ca-cert.pem
  
  2. Command to copy (run on WFM Server):
     scp ~/symphony/api/certificates/ca-cert.pem \\
         <device-agent-user>@<device-agent-ip>:~/sandbox/conformance/wfm-supplier/certs/

WHAT IT DOES:
  • The ca-cert.pem is used to verify WFM Server identity
  • Tests use this certificate to establish secure connections
  • Required for RFC 9421 HTTP Message Signature verification

EOF

    echo ""
    read -p "Press Enter once you have copied the certificate, or Ctrl+C to cancel: " continue_input
}

wfm_setup_miaf_identity() {
    local mis_script="$SCRIPT_DIR/ctt-mis.sh"
    if [[ ! -f "$mis_script" ]]; then
        error "ctt-mis.sh not found at $mis_script"
    fi

    while true; do
        echo ""
        echo "┌─────────────────────────────────────────────────────────────────────────┐"
        echo "│  MIAF Identity Setup — Where does the CTT's client SVID come from?      │"
        echo "├─────────────────────────────────────────────────────────────────────────┤"
        echo "│  1) MIS-issued identity       (copy the CTT's SVID issued by the MIS)   │"
        echo "│  2) CTT-generated identity    (ctt-mis.sh, when no MIS is running)      │"
        echo "│  B) Back                                                                │"
        echo "└─────────────────────────────────────────────────────────────────────────┘"
        echo ""
        read -p "Select option (1-2 or B): " miaf_choice < /dev/tty

        case "${miaf_choice,,}" in
            1) _wfm_miaf_vendor;                 return ;;
            2) _wfm_miaf_symphony "$mis_script"; return ;;
            b|back) return ;;
            *) warn "Invalid option — enter 1, 2, or B" ;;
        esac
    done
}

_wfm_miaf_symphony() {
    local mis_script="$1"
    local real_dir="$CONFORMANCE_DIR/wfm-supplier/utils/fixtures/miaf/real"

    echo ""
    info "Generating CTT device identity for Symphony testing..."
    info "Using trust domain: ctt-margo.org (matches Symphony's MIS config)"
    echo ""

    # Use ctt-margo.org trust domain to match Symphony's symphony-api-margo.json MIS config.
    # CTT_DIR auto-installs device SVID to fixture paths.
    TRUST_DOMAIN="ctt-margo.org" CTT_DIR="$CONFORMANCE_DIR" bash "$mis_script"
    # svid-gen.sh runs as sudo inside mis_script; fix ownership immediately so CTT can read the certs
    sudo chown -R "${USER}:${USER}" "$real_dir" 2>/dev/null || true

    # Read the SPIFFE ID from the installed cert
    local device_spiffe_id=""
    if [[ -f "$real_dir/client-svid-cert.pem" ]]; then
        device_spiffe_id=$(openssl x509 \
            -in "$real_dir/client-svid-cert.pem" \
            -text -noout 2>/dev/null \
            | grep -oE 'URI:spiffe://[^[:space:]]+' | head -1 | sed 's/URI://')
    fi

    echo ""
    echo "════════════════════════════════════════════════════════════════"
    echo "  CTT identity generated."
    echo ""
    [[ -n "$device_spiffe_id" ]] \
        && echo "  Device SPIFFE ID: ${device_spiffe_id}" \
        && echo "  (Add this to Symphony via wfm.sh → option 7)"
    echo ""
    echo "  Before starting Symphony, configure it to trust this identity"
    echo "  (WFM SVID, CA and trust bundle from ctt-mis.sh). The setup guide"
    echo "  covers the MIS-issued identity instead — option 1 of this menu."
    echo "════════════════════════════════════════════════════════════════"
}

_wfm_miaf_vendor() {
    local real_dir="$CONFORMANCE_DIR/wfm-supplier/utils/fixtures/miaf/real"

    echo ""
    echo "════════════════════════════════════════════════════════════════"
    echo "  MIS-issued identity for the CTT (acting as a WFM Client)"
    echo "════════════════════════════════════════════════════════════════"
    echo ""
    echo "  The MIS of the Trust Domain must have issued the CTT a WFM Client"
    echo "  X.509-SVID for the WFM under test:  <WFM SPIFFE ID>/client/<wfm-client-id>"
    echo ""
    echo "  Provide the folder that contains it. Accepted layouts:"
    echo "    • The SVID folder of the CTT (x509svid-<wfm-id>-<wfm-client-id>), or the"
    echo "      folder that contains the x509svid-* folders (sandbox:"
    echo "      \$HOME/mis-deployment). The SVID Root CA certificate is taken"
    echo "      from certs/ca.crt next to them or from \$HOME/mis-deployment/certs/ca.crt."
    echo "    • A flat folder with:"
    echo "        client-svid-cert.pem, client-svid-key.pem, trust-bundle-ca.pem (or mis-ca.crt)"
    echo ""
    echo "  Folders owned by root are read with sudo."
    echo ""

    _device_read_dir "Folder containing the client SVID" "$HOME/mis-deployment"
    local src_dir="${REPLY%/}"
    if [[ ! -d "$src_dir" ]] && ! sudo test -d "$src_dir"; then
        warn "Folder not found: $src_dir"
        return 1
    fi

    # The answer may be the client SVID's own folder (x509svid-…) instead of the
    # MIS output folder that holds it: use it, and take the CA from next to it.
    local picked_svid=""
    if _device_identity_file_exists "$src_dir/payload-cert.pem"; then
        picked_svid="$src_dir"
        src_dir="$(dirname "$picked_svid")"
        info "Using ${picked_svid##*/} as the client SVID (MIS output folder: $src_dir)"
    fi

    local cert_path key_path ca_path
    if _device_identity_file_exists "$src_dir/client-svid-cert.pem"; then
        cert_path="$src_dir/client-svid-cert.pem"
        key_path="$src_dir/client-svid-key.pem"
        ca_path="$(_identity_ca_file "$src_dir")"
    elif [[ -n "$picked_svid" ]]; then
        cert_path="$picked_svid/payload-cert.pem"
        key_path="$picked_svid/payload-key.pem"
        ca_path="$(_identity_ca_for_svid "$cert_path" "$src_dir")"
    else
        local find_cmd=(find "$src_dir" -mindepth 1 -maxdepth 1 -type d -name 'x509svid-*')
        [[ -r "$src_dir" && -x "$src_dir" ]] || find_cmd=(sudo "${find_cmd[@]}")
        local svid_dirs=()
        mapfile -t svid_dirs < <("${find_cmd[@]}" 2>/dev/null | sort)

        if (( ${#svid_dirs[@]} >= 1 )); then
            echo ""
            echo "  SVID folders found in $src_dir:"
            local i
            for i in "${!svid_dirs[@]}"; do
                echo "    $((i + 1))) ${svid_dirs[$i]##*/}"
            done
            echo ""
            # No default: the person who generated the SVIDs picks the CTT's client SVID by name.
            local client_n=""
            while [[ -z "$client_n" ]]; do
                read -r -p "  Which one is the CTT's client SVID?: " client_n < /dev/tty
            done
            if [[ ! "$client_n" =~ ^[0-9]+$ ]] || (( client_n < 1 || client_n > ${#svid_dirs[@]} )); then
                warn "Invalid selection — pick a number from the list."
                return 1
            fi
            cert_path="${svid_dirs[$((client_n - 1))]}/payload-cert.pem"
            key_path="${svid_dirs[$((client_n - 1))]}/payload-key.pem"
            ca_path="$(_identity_ca_for_svid "$cert_path" "$src_dir")"
        else
            echo ""
            echo "  No known layout found in $src_dir. Enter the three files:"
            read -r -p "  Path to client SVID cert  : " cert_path < /dev/tty
            read -r -p "  Path to client SVID key   : " key_path < /dev/tty
            read -r -p "  Path to SVID Root CA cert : " ca_path < /dev/tty
            cert_path="${cert_path/#\~/$HOME}"
            key_path="${key_path/#\~/$HOME}"
            ca_path="${ca_path/#\~/$HOME}"
        fi
    fi

    # MIS layout, but the CA is not where the MIS puts it (certs/ca.crt): ask for it.
    if [[ "$ca_path" == */certs/ca.crt ]] && ! _device_identity_file_exists "$ca_path"; then
        echo ""
        echo "  SVID Root CA certificate (trust anchor of the Trust Domain) not found at $ca_path"
        read -r -p "  Path to the SVID Root CA certificate (ca.crt of the MIS): " ca_path < /dev/tty
        ca_path="${ca_path/#\~/$HOME}"
    fi

    mkdir -p "$real_dir" 2>/dev/null || sudo mkdir -p "$real_dir" || return 1
    [[ -w "$real_dir" ]] || sudo chown -R "$(id -u):$(id -g)" "$real_dir/" || return 1

    # Stage and verify first, so a bad or incomplete set never replaces a working identity.
    local stage
    stage="$(mktemp -d "$real_dir/.identity.XXXXXX")" || return 1
    if ! { _device_copy_identity_file "$cert_path" "$stage/client-svid-cert.pem" 644 \
        && _device_copy_identity_file "$key_path"  "$stage/client-svid-key.pem"  600 \
        && _device_copy_identity_file "$ca_path"   "$stage/trust-bundle-ca.pem"  644; }; then
        rm -rf "$stage"
        warn "Identity setup aborted — existing certs in $real_dir were left untouched."
        return 1
    fi

    if ! openssl verify -CAfile "$stage/trust-bundle-ca.pem" "$stage/client-svid-cert.pem" >/dev/null 2>&1; then
        rm -rf "$stage"
        warn "The client X.509-SVID does not chain to the given SVID Root CA ($ca_path)."
        warn "Identity setup aborted — existing certs in $real_dir were left untouched."
        return 1
    fi

    local ctt_spiffe_id
    ctt_spiffe_id=$(device_spiffe_id "$stage/client-svid-cert.pem")
    if [[ "$ctt_spiffe_id" != *"/client/"* ]]; then
        rm -rf "$stage"
        warn "That is not a client SVID: ${ctt_spiffe_id:-no SPIFFE ID}"
        warn "Expected <WFM SPIFFE ID>/client/<wfm-client-id> — pick the client SVID, not the WFM's own."
        warn "Identity setup aborted — existing certs in $real_dir were left untouched."
        return 1
    fi

    # A re-issued SVID for the same identity is a rotation: keep the SVID being
    # replaced (while the WFM can still accept it), so the SVID Rotation scenario
    # can show the WFM takes both without the client being registered again.
    local rotated=0
    if [[ -f "$real_dir/client-svid-cert.pem" && -f "$real_dir/client-svid-key.pem" ]] \
        && ! cmp -s "$real_dir/client-svid-cert.pem" "$stage/client-svid-cert.pem"; then
        if [[ "$(device_spiffe_id "$real_dir/client-svid-cert.pem")" == "$ctt_spiffe_id" ]] \
            && openssl verify -CAfile "$stage/trust-bundle-ca.pem" "$real_dir/client-svid-cert.pem" >/dev/null 2>&1 \
            && openssl x509 -in "$real_dir/client-svid-cert.pem" -noout -checkend 0 >/dev/null 2>&1; then
            cp -f "$real_dir/client-svid-cert.pem" "$real_dir/previous-client-svid-cert.pem"
            cp -f "$real_dir/client-svid-key.pem"  "$real_dir/previous-client-svid-key.pem"
            rotated=1
        else
            rm -f "$real_dir/previous-client-svid-cert.pem" "$real_dir/previous-client-svid-key.pem"
        fi
    fi

    local f
    for f in client-svid-cert.pem client-svid-key.pem trust-bundle-ca.pem; do
        mv -f "$stage/$f" "$real_dir/$f"
    done
    rm -rf "$stage"

    echo ""
    success "Identity setup complete — client X.509-SVID validated against the Trust Domain's trust anchor."
    echo ""
    echo "  CTT client SVID : $ctt_spiffe_id"
    echo "  WFM it belongs to: ${ctt_spiffe_id%/client/*}"
    if [[ $rotated -eq 1 ]]; then
        echo ""
        echo "  This SVID replaces an earlier one for the same identity. The earlier SVID"
        echo "  was kept as the pre-rotation SVID — the SVID Rotation scenario will use both."
    elif [[ ! -f "$real_dir/previous-client-svid-cert.pem" ]]; then
        echo ""
        echo "  SVID Rotation scenario: not run until the client SVID is re-issued for this"
        echo "  same SPIFFE ID and this step is run again with the new SVID."
    fi
    echo ""
    echo "  ➜  The WFM under test must accept this client: add the CTT client SVID's"
    echo "     SPIFFE ID above in its local authorization policy before running the tests"
    echo "     (Symphony: wfm.sh → 7) Manage SPIFFE ID allowlist)."
    echo ""
}

run_wfm_flow() {

    while true; do
        echo ""
        echo "┌─────────────────────────────────────────────────────────────────────────┐"
        echo "│  WFM Supplier — Conformance Testing                                     │"
        echo "├─────────────────────────────────────────────────────────────────────────┤"
        echo "│  Run steps in order:                                                    │"
        echo "│    1. Setup MIAF Identity   (install CTT SVID + Root CA, once per WFM)  │"
        echo "│    2. Functional tests      (group-based test management)               │"
        echo "│    3. OpenAPI spec based contract tests                                 │"
        echo "│                                                                         │"
        echo "│  B) Back to main menu                                                   │"
        echo "└─────────────────────────────────────────────────────────────────────────┘"
        echo ""

        read -p "Select option (1-3, B, or Q): " test_choice

        case "${test_choice,,}" in

            1)
                wfm_setup_miaf_identity || true
                ;;

            2)
                echo ""
                info "Selecting test group..."

                local selected_group_path
                if selected_group_path=$(select_wfm_group); then
                    local group_name
                    group_name=$(basename "$selected_group_path")
                    success "Selected group: $group_name"

                    echo ""
                    read -p "Enter WFM URL: " wfm_url < /dev/tty
                    [[ -z "$wfm_url" ]] && error "The WFM URL is required"
                    miaf_url="${miaf_url:-$wfm_url}"

                    execute_wfm_tests_with_group "$wfm_url" "$selected_group_path" "$miaf_url"
                else
                    error "Failed to select group"
                fi
                ;;

            3)
                echo ""
                local default_collection
                default_collection=$(realpath "$CONFORMANCE_DIR/../ctt-creator/wfm-supplier/utils/postman_collection.json" 2>/dev/null)
                echo "  The collection generated from the OpenAPI file (ctt-creator → WFM Supplier →"
                echo "  OpenAPI spec based contract tests). Press Enter for the one shipped with the CTT."
                read -p "Enter Postman Collection Path [$default_collection]: " collection_path < /dev/tty
                collection_path="${collection_path:-$default_collection}"
                collection_path="${collection_path/#\~/$HOME}"

                if [[ ! -f "$collection_path" ]]; then
                    error "Collection file not found: $collection_path"
                fi

                echo ""
                read -p "Enter WFM URL: " wfm_url < /dev/tty
                [[ -z "$wfm_url" ]] && error "The WFM URL is required"

                run_wfm_contract_collection "$wfm_url" "$collection_path"
                ;;

            b)
                return
                ;;

            q)
                info "Exiting..."
                exit 0
                ;;

            *)
                warn "Invalid option"
                ;;
        esac

        echo ""
        read -p "Press Enter to continue..." _ < /dev/tty
    done
}

# True if the file exists; checks with sudo when its directory is not accessible to the current user.
_device_identity_file_exists() {
    [[ -f "$1" ]] && return 0
    local dir="${1%/*}"
    [[ -r "$dir" && -x "$dir" ]] && return 1
    sudo test -f "$1"
}

# The SVID Root CA for SVID folders in the MIS layout. The sandbox writes the
# x509svid-* folders into the directory mis.sh was run from, while the Root CA
# stays in the MIS deployment folder, so look in both and take the one the
# given SVID chains to.
_identity_ca_for_svid() {
    local svid_cert="$1" base="$2" cand first=""
    for cand in "$base/certs/ca.crt" "$HOME/mis-deployment/certs/ca.crt"; do
        _device_identity_file_exists "$cand" || continue
        [[ -z "$first" ]] && first="$cand"
        if sudo -n true 2>/dev/null || [[ -r "$cand" && -r "$svid_cert" ]]; then
            if { [[ -r "$cand" && -r "$svid_cert" ]] && openssl verify -CAfile "$cand" "$svid_cert" >/dev/null 2>&1; } \
               || sudo -n openssl verify -CAfile "$cand" "$svid_cert" >/dev/null 2>&1; then
                echo "$cand"
                return 0
            fi
        fi
    done
    echo "${first:-$base/certs/ca.crt}"
}

# The MIS CA in a flat identity folder: the same folder can serve both personas,
# so any of the names the guides use is accepted.
_identity_ca_file() {
    local name
    for name in trust-bundle-ca.pem mis-ca.crt ca.crt; do
        if _device_identity_file_exists "$1/$name"; then
            echo "$1/$name"
            return 0
        fi
    done
    echo "$1/mis-ca.crt"
}

# Copies one identity file so the current user owns it; uses sudo only when the source is not readable.
_device_copy_identity_file() {
    local src="$1" dst="$2" mode="$3"
    if ! _device_identity_file_exists "$src"; then
        warn "Missing file: $src"
        return 1
    fi
    if [[ -r "$src" ]]; then
        cp "$src" "$dst" || return 1
    else
        sudo cp "$src" "$dst" || return 1
        sudo chown "$(id -u):$(id -g)" "$dst" || return 1
    fi
    chmod "$mode" "$dst"
}

# Prompts for a folder, expands a leading ~ and strips a trailing slash. Result in REPLY.
_device_read_dir() {
    local prompt="$1" default="$2" answer
    if [[ -n "$default" ]]; then
        read -r -p "  $prompt [$default]: " answer < /dev/tty
    else
        read -r -p "  $prompt: " answer < /dev/tty
    fi
    answer="${answer:-$default}"
    answer="${answer/#\~/$HOME}"
    REPLY="${answer%/}"
}

device_generate_certs() {
    local device_dir="$CONFORMANCE_DIR/device-supplier"
    local cert_dir="$device_dir/certs"

    echo ""
    echo "  ── MIAF Identity Setup ──────────────────────────────────────────────────"
    echo "  The MIS of the Trust Domain must have issued two X.509-SVIDs:"
    echo "    1. A WFM SVID        — identity of the mock WFM (mTLS endpoint, port 3003)"
    echo "    2. A WFM Client SVID — identity of the CTT test client"
    echo ""
    echo "  Provide the folder that contains them. Accepted layouts:"
    echo "    • The folder that contains the x509svid-* folders (sandbox:"
    echo "      \$HOME/mis-deployment). The SVID Root CA certificate is taken"
    echo "      from certs/ca.crt next to them or from \$HOME/mis-deployment/certs/ca.crt."
    echo "    • A flat folder with:"
    echo "        wfm-svid-cert.pem, wfm-svid-key.pem,"
    echo "        client-svid-cert.pem, client-svid-key.pem, mis-ca.crt (or trust-bundle-ca.pem)"
    echo ""
    echo "  Folders owned by root are read with sudo."
    echo "  The device under test must belong to the same Trust Domain (same MIS)."
    echo "  ────────────────────────────────────────────────────────────────────────"
    echo ""

    _device_read_dir "Folder containing the SVIDs" "$HOME/mis-deployment"
    local src_dir="${REPLY%/}"
    if [[ ! -d "$src_dir" ]] && ! sudo test -d "$src_dir"; then
        warn "Folder not found: $src_dir"
        return 1
    fi

    # The answer may be one SVID's own folder (x509svid-…): two SVIDs are needed,
    # so continue with the MIS output folder that holds it.
    if _device_identity_file_exists "$src_dir/payload-cert.pem"; then
        info "${src_dir##*/} is one SVID folder — using the folder that holds it: $(dirname "$src_dir")"
        src_dir="$(dirname "$src_dir")"
    fi

    local wfm_cert wfm_key client_cert client_key ca_cert

    if _device_identity_file_exists "$src_dir/wfm-svid-cert.pem"; then
        wfm_cert="$src_dir/wfm-svid-cert.pem"
        wfm_key="$src_dir/wfm-svid-key.pem"
        client_cert="$src_dir/client-svid-cert.pem"
        client_key="$src_dir/client-svid-key.pem"
        ca_cert="$(_identity_ca_file "$src_dir")"
    else
        local find_cmd=(find "$src_dir" -mindepth 1 -maxdepth 1 -type d -name 'x509svid-*')
        [[ -r "$src_dir" && -x "$src_dir" ]] || find_cmd=(sudo "${find_cmd[@]}")
        local svid_dirs=()
        mapfile -t svid_dirs < <("${find_cmd[@]}" 2>/dev/null | sort)

        local wfm_dir client_dir
        if (( ${#svid_dirs[@]} >= 2 )); then
            echo ""
            echo "  SVID folders found in $src_dir:"
            local i
            for i in "${!svid_dirs[@]}"; do
                echo "    $((i + 1))) ${svid_dirs[$i]##*/}"
            done
            echo ""
            # No defaults: the person who generated the SVIDs picks them by name.
            local wfm_n="" client_n=""
            while [[ -z "$wfm_n" ]]; do
                read -r -p "  Select the mock WFM SVID: " wfm_n < /dev/tty
            done
            while [[ -z "$client_n" ]]; do
                read -r -p "  Select the WFM Client SVID: " client_n < /dev/tty
            done
            if [[ ! "$wfm_n" =~ ^[0-9]+$ || ! "$client_n" =~ ^[0-9]+$ ]] \
                || (( wfm_n < 1 || wfm_n > ${#svid_dirs[@]} )) \
                || (( client_n < 1 || client_n > ${#svid_dirs[@]} )) \
                || (( wfm_n == client_n )); then
                warn "Invalid selection — pick two different numbers from the list."
                return 1
            fi
            wfm_dir="${svid_dirs[$((wfm_n - 1))]}"
            client_dir="${svid_dirs[$((client_n - 1))]}"
            ca_cert="$(_identity_ca_for_svid "$client_dir/payload-cert.pem" "$src_dir")"
        else
            echo ""
            echo "  No known layout found in $src_dir."
            echo "  Enter the folders holding payload-cert.pem / payload-key.pem for each SVID:"
            _device_read_dir "WFM SVID folder" "";    wfm_dir="$REPLY"
            _device_read_dir "Client SVID folder" ""; client_dir="$REPLY"
            read -r -p "  SVID Root CA certificate file (ca.crt of the MIS): " ca_cert < /dev/tty
            ca_cert="${ca_cert/#\~/$HOME}"
        fi
        wfm_cert="$wfm_dir/payload-cert.pem"
        wfm_key="$wfm_dir/payload-key.pem"
        client_cert="$client_dir/payload-cert.pem"
        client_key="$client_dir/payload-key.pem"
    fi

    mkdir -p "$cert_dir" 2>/dev/null || sudo mkdir -p "$cert_dir" || return 1
    [[ -w "$cert_dir" ]] || sudo chown -R "$(id -u):$(id -g)" "$cert_dir/" || return 1

    # Stage and verify first, so a bad or incomplete set never replaces a working identity.
    local stage
    stage="$(mktemp -d "$cert_dir/.identity.XXXXXX")" || return 1
    if ! { _device_copy_identity_file "$wfm_cert"    "$stage/miaf-server-cert.pem" 644 \
        && _device_copy_identity_file "$wfm_key"     "$stage/miaf-server-key.pem"  600 \
        && _device_copy_identity_file "$client_cert" "$stage/svid-cert.pem"        644 \
        && _device_copy_identity_file "$client_key"  "$stage/svid-key.pem"         600 \
        && _device_copy_identity_file "$ca_cert"     "$stage/svid-ca.pem"          644; }; then
        rm -rf "$stage"
        warn "Identity setup aborted — existing certs in $cert_dir were left untouched."
        return 1
    fi

    local f
    for f in miaf-server-cert.pem svid-cert.pem; do
        if ! openssl verify -CAfile "$stage/svid-ca.pem" "$stage/$f" >/dev/null 2>&1; then
            rm -rf "$stage"
            warn "$f does not chain to the given SVID Root CA ($ca_cert)."
            warn "Identity setup aborted — existing certs in $cert_dir were left untouched."
            return 1
        fi
    done

    # The spec ties the two identities together: the client SVID must be
    # <WFM SPIFFE ID>/client/<wfm-client-id>.
    local wfm_spiffe_id client_spiffe_id
    wfm_spiffe_id=$(device_spiffe_id "$stage/miaf-server-cert.pem")
    client_spiffe_id=$(device_spiffe_id "$stage/svid-cert.pem")
    if [[ -z "$wfm_spiffe_id" || "$wfm_spiffe_id" == *"/client/"* || "$client_spiffe_id" != "$wfm_spiffe_id/client/"* ]]; then
        rm -rf "$stage"
        warn "The client SVID is not a client of the WFM SVID:"
        warn "  WFM SVID    : ${wfm_spiffe_id:-no SPIFFE ID}"
        warn "  Client SVID : ${client_spiffe_id:-no SPIFFE ID}"
        warn "Expected the client to be <WFM SPIFFE ID>/client/<wfm-client-id>. Check which one is the WFM"
        warn "and which is the client, and that both were generated for the same WFM ID."
        warn "Identity setup aborted — existing certs in $cert_dir were left untouched."
        return 1
    fi

    # A rotation: the client SVID is replaced by another one for the same SPIFFE
    # ID. The replaced SVID is kept (while still valid and from the same MIS CA)
    # so the SVID Rotation scenario can present it as the pre-rotation identity.
    local rotation_note="not run until the client SVID is re-issued for this same SPIFFE ID and this step is run again"
    if [[ -f "$cert_dir/svid-cert.pem" && -f "$cert_dir/svid-key.pem" ]] \
       && ! cmp -s "$cert_dir/svid-cert.pem" "$stage/svid-cert.pem"; then
        if [[ "$(device_spiffe_id "$cert_dir/svid-cert.pem")" == "$client_spiffe_id" ]] \
           && openssl verify -CAfile "$stage/svid-ca.pem" "$cert_dir/svid-cert.pem" >/dev/null 2>&1 \
           && openssl x509 -in "$cert_dir/svid-cert.pem" -noout -checkend 0 >/dev/null 2>&1; then
            cp -f "$cert_dir/svid-cert.pem" "$cert_dir/svid-previous-cert.pem"
            cp -f "$cert_dir/svid-key.pem"  "$cert_dir/svid-previous-key.pem"
        else
            rm -f "$cert_dir/svid-previous-cert.pem" "$cert_dir/svid-previous-key.pem"
        fi
    fi
    [[ -f "$cert_dir/svid-previous-cert.pem" ]] && rotation_note="ready — the replaced client SVID was kept as the pre-rotation identity"

    for f in miaf-server-cert.pem miaf-server-key.pem svid-cert.pem svid-key.pem svid-ca.pem; do
        mv -f "$stage/$f" "$cert_dir/$f"
    done
    rm -rf "$stage"

    echo ""
    success "Identity setup complete — both X.509-SVIDs validated against the Trust Domain's trust anchor."
    echo ""
    echo "  Certs       : $cert_dir"
    echo "  WFM SVID    : $wfm_spiffe_id"
    echo "  Client SVID : $client_spiffe_id"
    echo "  SVID Rotation scenario: $rotation_note."
    echo ""
    echo "  ➜  A WFM Client tested against this mock WFM must use the Trust Bundle of the same MIS"
    echo "     ($cert_dir/svid-ca.pem) and hold an SVID of the form"
    echo "     $wfm_spiffe_id/client/<wfm-client-id>."
    echo ""
}

# Builds both device-supplier binaries from the current source (so a stale
# bin/ never outlives a 'git pull') and links the folders the mock WFM reads
# at runtime. Run from the device-supplier directory.
device_prepare_runtime() {
    if ! command -v go &> /dev/null; then
        error "Go not found. Install from https://golang.org/doc/install"
    fi

    log "📦 Building mock WFM server and test runner..."
    go build -o bin/server ./scripts/cmd/device-supplier || error "Failed to build mock server"
    go build -o bin/run_tests ./scripts || error "Failed to build test runner"

    local dir
    for dir in manifests sample-apps; do
        [[ -e "$dir" ]] || ln -s "utils/$dir" "$dir"
    done
}

# True when the MIS-issued identity from 'Setup Identity' is in place.
device_identity_ready() {
    local cert_dir="$1" f
    for f in miaf-server-cert.pem miaf-server-key.pem svid-cert.pem svid-key.pem svid-ca.pem; do
        [[ -f "$cert_dir/$f" ]] || return 1
    done
}

# Prints the SPIFFE ID (URI SAN) of a certificate.
device_spiffe_id() {
    openssl x509 -in "$1" -noout -ext subjectAltName 2>/dev/null | grep -o 'spiffe://[^, ]*' | head -1
}

# Stops any mock WFM left over from an earlier run.
device_stop_stale_server() {
    if [[ -f /tmp/wfm-server.pid ]]; then
        local old_pid
        old_pid=$(cat /tmp/wfm-server.pid)
        if kill -0 "$old_pid" 2>/dev/null; then
            log "⛔ Stopping previous server instance (PID: $old_pid)..."
            kill -15 "$old_pid" 2>/dev/null
            sleep 1
        fi
        rm -f /tmp/wfm-server.pid
    fi

    if command -v lsof &> /dev/null; then
        local port pid_on_port
        for port in 3001 3003; do
            pid_on_port=$(lsof -ti ":$port" -sTCP:LISTEN 2>/dev/null || true)
            if [[ -n "$pid_on_port" ]]; then
                log "⛔ Stopping process on port $port (PID: $pid_on_port)..."
                kill -15 $pid_on_port 2>/dev/null
                sleep 1
            fi
        done
    fi
}

# Starts the mock WFM in the background: the Management Interface on the mTLS
# port (3003) with the MIS-issued WFM SVID, trusting only the MIS CA. nohup
# keeps it running after the menu or the terminal session closes.
# Run from the device-supplier directory.
device_launch_server() {
    local cert_dir="$1"
    MIAF_SERVER_CERT="$cert_dir/miaf-server-cert.pem" \
    MIAF_SERVER_KEY="$cert_dir/miaf-server-key.pem" \
    MIAF_TRUST_CA="$cert_dir/svid-ca.pem" \
        nohup ./bin/server > /tmp/wfm-server.log 2>&1 &
    local server_pid=$!
    echo "$server_pid" > /tmp/wfm-server.pid
    sleep 2

    if ! kill -0 "$server_pid" 2>/dev/null; then
        rm -f /tmp/wfm-server.pid
        tail -5 /tmp/wfm-server.log >&2
        error "Failed to start mock server. Full log: /tmp/wfm-server.log"
    fi
}

# The address a device uses to reach this host: CTT_HOST_ADDRESS when set,
# otherwise the source address of the default route (not a Docker/k3s bridge),
# otherwise the first address of `hostname -I`, otherwise localhost.
ctt_host_address() {
    if [[ -n "${CTT_HOST_ADDRESS:-}" ]]; then
        echo "$CTT_HOST_ADDRESS"; return
    fi
    local addr
    addr=$(ip route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i < NF; i++) if ($i == "src") {print $(i + 1); exit}}')
    [[ -z "$addr" ]] && addr=$(hostname -I 2>/dev/null | awk '{print $1}')
    echo "${addr:-localhost}"
}

device_start_server() {
    local device_dir="$CONFORMANCE_DIR/device-supplier"
    local cert_dir="$device_dir/certs"

    if ! device_identity_ready "$cert_dir"; then
        warn "MIAF identity not found at $cert_dir. Please run 'Setup Identity' first (option 1)."
        return 1
    fi

    cd "$device_dir"
    device_prepare_runtime
    device_stop_stale_server

    log "🚀 Starting Mock WFM Server..."
    device_launch_server "$cert_dir"

    local server_host
    server_host=$(ctt_host_address)

    echo ""
    success "Mock WFM Server is running (PID: $(cat /tmp/wfm-server.pid))"
    echo ""
    echo "  Mock WFM (Margo rc.3 — mTLS with X.509-SVID)"
    echo "  ─────────────────────────────────────────────────────────────────────"
    echo "  Management Interface : https://${server_host}:3003/v1alpha2/margo"
    echo "  WFM SPIFFE ID        : $(device_spiffe_id "$cert_dir/miaf-server-cert.pem")"
    echo "  Trust anchor (MIS)   : $cert_dir/svid-ca.pem"
    echo "  Health / telemetry   : https://${server_host}:3001/health"
    echo "  Server log           : /tmp/wfm-server.log"
    echo ""
    echo "  ➜  Option 3 (Run Tests) runs the CTT's own device-agent against this server."
    echo "  ➜  To test a real device: point it at the Management Interface URL above."
    echo "     The device must reach this host at that address; to show a different"
    echo "     address (e.g. the host's IP on the device network): CTT_HOST_ADDRESS=<ip-or-name>"
    echo "     Its SVID must be issued by the same MIS, as"
    echo "     <WFM SPIFFE ID>/client/<wfm-client-id>, and it must accept the WFM SPIFFE ID."
    echo ""
}

device_run_tests() {
    local device_dir="$CONFORMANCE_DIR/device-supplier"

    # Check server is running
    if [[ ! -f /tmp/wfm-server.pid ]] || ! kill -0 "$(cat /tmp/wfm-server.pid)" 2>/dev/null; then
        warn "Mock WFM Server is not running. Please start it first (option 2)."
        return 1
    fi

    cd "$device_dir"

    # Group selection
    echo ""
    info "Select the test group to validate..."
    local device_group
    if ! device_group=$(select_device_group); then
        warn "No group selected."
        return 1
    fi

    local group_name
    group_name=$(basename "$device_group")

    local group_scenarios
    group_scenarios=$(create_temp_scenarios_file)
    build_device_group_scenarios "$device_group" "$group_scenarios"

    # Stage scenarios for test runner
    log "📋 Staging test scenarios for group: $group_name..."
    mkdir -p ./device-scenarios
    cp "$group_scenarios" ./device-scenarios/test-scenarios.json
    rm -f "$group_scenarios"

    # Groups may opt into flexible-order mode via a "flexibleOrder" key in
    # group.json — runs the fixed_first scenario first, then the rest in random
    # order. Absent/false for every existing group, so this is a no-op for them.
    local extra_flags=("-group" "$(basename "$device_group")")
    if [[ "$(jq -r '.flexibleOrder // false' "$device_group/group.json" 2>/dev/null)" == "true" ]]; then
        extra_flags+=("-flexible-order")
        info "🔀 Flexible-order mode enabled for group '$group_name'."
    fi

    local claimed_app_version
    claimed_app_version=$(jq -r '.version // "unknown"' "$device_group/group.json" 2>/dev/null)
    local ctt_margo_version
    ctt_margo_version=$(get_ctt_margo_version)
    extra_flags+=("-claimed-app-version" "$claimed_app_version" "-ctt-margo-version" "$ctt_margo_version")
    confirm_version_mismatch "$claimed_app_version"

    # The CTT reaches its own mock WFM on this host (fixed ports), so the
    # defaults are always right; the prompts only show which URLs are used.
    local default_url="https://localhost:3001/v1alpha2/margo"
    local default_miaf_url="https://localhost:3003/v1alpha2/margo"
    local wfm_url miaf_url
    echo ""
    echo "  ➜  The CTT connects to its own mock WFM on this host. Press Enter at both prompts."
    read -p "Enter Mock WFM Server URL [$default_url]: " wfm_url < /dev/tty
    wfm_url="${wfm_url:-$default_url}"
    read -p "Enter Management Interface (mTLS) URL [$default_miaf_url]: " miaf_url < /dev/tty
    miaf_url="${miaf_url:-$default_miaf_url}"
    extra_flags+=("-miaf-url" "$miaf_url")

    log "▶️  Running Device Conformance Tests"
    log "   Mock WFM (health):           $wfm_url"
    log "   Management Interface (mTLS): $miaf_url"
    echo ""

    local test_result=0
    if ./bin/run_tests -url "$wfm_url" "${extra_flags[@]}" 2>&1 | tee "$RUNNER_DEVICE/test-execution.log"; then
        test_result=0
    else
        test_result=1
    fi

    # Copy report to Runner output directory
    local latest_report
    latest_report=$(find reports -name "conformance-report-*.html" -type f -printf '%T@ %p\n' 2>/dev/null | sort -rn | head -1 | cut -d' ' -f2- || true)

    if [[ -n "$latest_report" && -f "$latest_report" ]]; then
        cp "$latest_report" "$RUNNER_DEVICE/"
        success "Report: $RUNNER_DEVICE/$(basename "$latest_report")"
        sign_report "$RUNNER_DEVICE/$(basename "$latest_report")"
    else
        info "Reports location: $device_dir/reports/"
    fi

    echo ""
    if [[ $test_result -ne 0 ]]; then
        warn "Some tests failed. Check: $RUNNER_DEVICE/test-execution.log"
    else
        success "All device conformance tests passed!"
    fi
}

device_stop_server() {
    local stopped=0

    if [[ -f /tmp/wfm-server.pid ]]; then
        local pid
        pid=$(cat /tmp/wfm-server.pid)
        if kill -0 "$pid" 2>/dev/null; then
            log "⛔ Stopping Mock WFM Server (PID: $pid)..."
            kill -15 "$pid" 2>/dev/null
            sleep 1
            if ! kill -0 "$pid" 2>/dev/null; then
                success "Mock WFM Server stopped."
                stopped=1
            else
                warn "Server did not stop cleanly. Try: kill -9 $pid"
            fi
        else
            info "PID $pid is no longer active."
        fi
        rm -f /tmp/wfm-server.pid
    fi

    # Also clear anything still listening on the mock WFM ports
    if command -v lsof &> /dev/null; then
        local port pid_on_port
        for port in 3001 3003; do
            pid_on_port=$(lsof -ti ":$port" -sTCP:LISTEN 2>/dev/null || true)
            if [[ -n "$pid_on_port" ]]; then
                log "⛔ Stopping remaining process on port $port (PID: $pid_on_port)..."
                kill -15 $pid_on_port 2>/dev/null
                sleep 1
                stopped=1
            fi
        done
    fi

    if [[ $stopped -eq 0 ]]; then
        info "No Mock WFM Server was running."
    fi
}

run_device_flow() {
    while true; do
        # Show live server status in the menu header
        local server_status="⛔ Stopped"
        if [[ -f /tmp/wfm-server.pid ]] && kill -0 "$(cat /tmp/wfm-server.pid)" 2>/dev/null; then
            local _running_pid
            _running_pid=$(cat /tmp/wfm-server.pid)
            server_status="✅ Running (PID: $_running_pid)"
        fi

        echo ""
        echo "┌─────────────────────────────────────────────────────────────────────────┐"
        echo "│         Device Supplier - Conformance Testing (MIAF / rc.3)              │"
        echo "│  Mock WFM Server: $server_status"
        echo "├─────────────────────────────────────────────────────────────────────────┤"
        echo "│  Run steps in order:                                                     │"
        echo "│    1. Setup Identity         (copy MIS-issued SVIDs, run once per setup) │"
        echo "│    2. Start Mock WFM Server  (Management Interface, mTLS on :3003)       │"
        echo "│    3. Run Tests              (select group)                              │"
        echo "│    4. Stop Mock WFM Server                                               │"
        echo "│                                                                          │"
        echo "│  B) Back to main menu                                                    │"
        echo "└─────────────────────────────────────────────────────────────────────────┘"
        echo ""

        read -p "Select option (1-4 or B): " device_choice < /dev/tty

        case "${device_choice,,}" in
            1) device_generate_certs || true ;;
            2) device_start_server || true ;;
            3) device_run_tests || true ;;
            4) device_stop_server || true ;;
            b|back)
                return 0
                ;;
            *)
                warn "Invalid option. Please select 1-4 or B."
                ;;
        esac

        echo ""
        read -p "Press Enter to continue..." _ < /dev/tty
    done
}

################################################################################
# Interactive Mode
################################################################################

interactive_mode() {
    while true; do
        show_persona_menu
        
        read -p "Select option (1-3, P, H, or Q): " choice

        case "${choice,,}" in
            1|wfm)
                echo ""
                info "You selected: WFM Supplier"
                run_wfm_flow
                ;;
            2|device)
                echo ""
                info "You selected: Device Supplier"
                run_device_flow
                ;;
            3|application)
                echo ""
                info "You selected: Application Supplier"
                run_application_supplier
                ;;
            p|prereq|prerequisites)
                check_prerequisites || true
                ;;
            h|help)
                show_help
                ;;
            q|quit)
                info "Exiting..."
                exit 0
                ;;
            *)
                error "Invalid option. Please select 1, 2,3, P, H, or Q"
                ;;
        esac
        
        echo ""
        read -p "Press Enter to continue or Q to quit: " continue_choice
        if [[ "${continue_choice,,}" == "q" ]]; then
            info "Exiting..."
            exit 0
        fi
        clear
    done
}

################################################################################
# Main Entry Point
################################################################################

main() {
    cat << 'EOF'
╔═══════════════════════════════════════════════════════════════════════════╗
║           Margo Conformance Test Runner                                   ║
║              Execute conformance tests and generate reports                ║
╚═══════════════════════════════════════════════════════════════════════════╝

EOF

    # No arguments - show interactive menu
    if [[ $# -eq 0 ]]; then
        interactive_mode
        return 0
    fi

    # Command line argument parsing
    local command="${1,,}"
    
    case "$command" in
        wfm)
            if [[ -n "${2:-}" ]]; then
                local group_path
                group_path=$(resolve_group_path "$WFM_GROUP_DIR" "$2") || \
                    error "WFM group not found: $2"
                execute_wfm_tests_with_group "${3:-}" "$group_path"
            else
                run_wfm_flow
            fi
            ;;
        device)
            if [[ -f "${2:-}" ]]; then
                execute_device_tests "$2"
            elif [[ -n "${2:-}" ]]; then
                local group_path
                group_path=$(resolve_group_path "$DEVICE_GROUP_DIR" "$2") || \
                    error "Device group or scenarios file not found: $2"
                local group_scenarios
                group_scenarios=$(create_temp_scenarios_file)
                build_device_group_scenarios "$group_path" "$group_scenarios"
                execute_device_tests "$group_scenarios" "$group_path"
                rm -f "$group_scenarios"
            else
                run_device_flow
            fi
            ;;
        3|application)
            run_application_supplier
            ;;
        help|-h|--help)
            show_help
            ;;
        *)
            error "Unknown command: $command

Usage: ../ctt-runner/ctt-start.sh [wfm|device|application|help]

Run '../ctt-runner/ctt-start.sh help' for detailed instructions."
            ;;
    esac
}

# Run main function
main "$@"
