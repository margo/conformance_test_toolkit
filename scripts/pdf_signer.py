#!/usr/bin/env python3
"""
HTML to Signed PDF Converter
------------------------------
Converts an HTML file to PDF using WeasyPrint, then digitally signs the PDF
using pyHanko. Supports both provided certificates (.pfx/.p12) and
auto-generated self-signed CA + end-entity certificates.

Dependencies:
    pip install weasyprint pyhanko pyhanko-certvalidator cryptography
"""

import argparse
import logging
import os
import io
import sys
from datetime import datetime, timezone, timedelta
from pathlib import Path
import tempfile

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.hazmat.primitives.serialization import pkcs12
from cryptography.x509.oid import NameOID

from pyhanko.pdf_utils.incremental_writer import IncrementalPdfFileWriter
from pyhanko.sign import signers, fields
from pyhanko.sign.fields import SigFieldSpec
from pyhanko.sign.signers.pdf_signer import PdfSignatureMetadata

from weasyprint import HTML

# ---------------------------------------------------------------------------
# Logging setup
# ---------------------------------------------------------------------------
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Self-signed certificate generation defaults
# (Edit these variables to customise the generated certificate fields)
# ---------------------------------------------------------------------------
CERT_COMMON_NAME       = "Margo Conformance Test Suite Report Certificate"
CERT_ORGANIZATION      = "Margo"
CERT_ORGANIZATIONAL_UNIT = "Conformance Test Toolkit"
CERT_COUNTRY           = "IN"          # Must be a 2-letter ISO code
CERT_STATE             = "Haryana"
CERT_LOCALITY          = "Gurugram"
CERT_EMAIL             = "nitin.parihar@capgemini.com"
CERT_VALIDITY_DAYS     = 365           # How long the generated cert is valid

CA_COMMON_NAME         = "Margo Conformance Test Suite Report Self-Signed Root CA"
CA_ORGANIZATION        = CERT_ORGANIZATION
CA_VALIDITY_DAYS       = 3650          # 10 years for the CA

# ---------------------------------------------------------------------------
# PDF signature metadata defaults
# (Edit these variables to customise the visible/embedded signature fields)
# ---------------------------------------------------------------------------
SIG_FIELD_NAME  = "Signature"         # Name of the signature form field in the PDF
SIG_REASON      = "Test Report Approval"  # Reason for signing
SIG_LOCATION    = "Haryana, IN"  # Location of the signer
SIG_CONTACT     = CERT_EMAIL           # Contact info embedded in the signature

# ---------------------------------------------------------------------------
# Output file name defaults
# ---------------------------------------------------------------------------
OUTPUT_CA_CERT_FILE  = "ca_cert.pem"   # Exported CA certificate (PEM)
OUTPUT_SIGNED_SUFFIX = "_signed"       # Appended to the base PDF name


FONTS_DIR = Path(__file__).parent / "fonts"
NOTO_EMOJI_FONT = FONTS_DIR / "NotoColorEmoji.ttf"


# ===========================================================================
# Certificate generation helpers
# ===========================================================================

def _generate_rsa_key(key_size: int = 2048):
    """Generate an RSA private key."""
    return rsa.generate_private_key(
        public_exponent=65537,
        key_size=key_size,
    )


def _build_name(cn: str, org: str, ou: str, country: str,
                state: str, locality: str, email: str) -> x509.Name:
    """Build an x509.Name from individual components."""
    return x509.Name([
        x509.NameAttribute(NameOID.COUNTRY_NAME,             country),
        x509.NameAttribute(NameOID.STATE_OR_PROVINCE_NAME,   state),
        x509.NameAttribute(NameOID.LOCALITY_NAME,            locality),
        x509.NameAttribute(NameOID.ORGANIZATION_NAME,        org),
        x509.NameAttribute(NameOID.ORGANIZATIONAL_UNIT_NAME, ou),
        x509.NameAttribute(NameOID.COMMON_NAME,              cn),
        x509.NameAttribute(NameOID.EMAIL_ADDRESS,            email),
    ])


def generate_self_signed_ca():
    """
    Generate a self-signed Root CA certificate and its private key.

    Returns:
        tuple: (ca_key, ca_cert) — RSA private key and x509 Certificate objects.
    """
    logger.info("Generating self-signed Root CA key and certificate …")
    ca_key = _generate_rsa_key()

    ca_name = _build_name(
        cn=CA_COMMON_NAME,
        org=CA_ORGANIZATION,
        ou="Certificate Authority",
        country=CERT_COUNTRY,
        state=CERT_STATE,
        locality=CERT_LOCALITY,
        email=CERT_EMAIL,
    )

    now = datetime.now(timezone.utc)
    ca_cert = (
        x509.CertificateBuilder()
        .subject_name(ca_name)
        .issuer_name(ca_name)                          # Self-signed → issuer == subject
        .public_key(ca_key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now)
        .not_valid_after(now + timedelta(days=CA_VALIDITY_DAYS))
        .add_extension(x509.BasicConstraints(ca=True, path_length=None), critical=True)
        .add_extension(
            x509.KeyUsage(
                digital_signature=True, key_cert_sign=True, crl_sign=True,
                content_commitment=False, key_encipherment=False,
                data_encipherment=False, key_agreement=False,
                encipher_only=False, decipher_only=False,
            ),
            critical=True,
        )
        .add_extension(
            x509.SubjectKeyIdentifier.from_public_key(ca_key.public_key()),
            critical=False,
        )
        .sign(ca_key, hashes.SHA256())
    )

    logger.info("Root CA certificate generated (CN=%s).", CA_COMMON_NAME)
    return ca_key, ca_cert


def generate_end_entity_cert(ca_key, ca_cert):
    """
    Generate an end-entity (leaf) certificate signed by the provided CA.

    Args:
        ca_key:  CA private key used to sign the new certificate.
        ca_cert: CA certificate (issuer).

    Returns:
        tuple: (ee_key, ee_cert) — RSA private key and x509 Certificate objects.
    """
    logger.info("Generating end-entity signing certificate …")
    ee_key = _generate_rsa_key()

    ee_name = _build_name(
        cn=CERT_COMMON_NAME,
        org=CERT_ORGANIZATION,
        ou=CERT_ORGANIZATIONAL_UNIT,
        country=CERT_COUNTRY,
        state=CERT_STATE,
        locality=CERT_LOCALITY,
        email=CERT_EMAIL,
    )

    now = datetime.now(timezone.utc)
    ee_cert = (
        x509.CertificateBuilder()
        .subject_name(ee_name)
        .issuer_name(ca_cert.subject)
        .public_key(ee_key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now)
        .not_valid_after(now + timedelta(days=CERT_VALIDITY_DAYS))
        .add_extension(x509.BasicConstraints(ca=False, path_length=None), critical=True)
        .add_extension(
            x509.KeyUsage(
                digital_signature=True, content_commitment=True,
                key_encipherment=False, data_encipherment=False,
                key_agreement=False, key_cert_sign=False,
                crl_sign=False, encipher_only=False, decipher_only=False,
            ),
            critical=True,
        )
        .add_extension(
            x509.ExtendedKeyUsage([x509.ExtendedKeyUsageOID.EMAIL_PROTECTION]),
            critical=False,
        )
        .add_extension(
            x509.SubjectKeyIdentifier.from_public_key(ee_key.public_key()),
            critical=False,
        )
        .add_extension(
            x509.AuthorityKeyIdentifier.from_issuer_public_key(ca_key.public_key()),
            critical=False,
        )
        .sign(ca_key, hashes.SHA256())
    )

    logger.info("End-entity certificate generated (CN=%s).", CERT_COMMON_NAME)
    return ee_key, ee_cert


def export_ca_cert_pem(ca_cert, output_path: Path) -> None:
    """Write the CA certificate to a PEM file so verifiers can import it."""
    pem_bytes = ca_cert.public_bytes(serialization.Encoding.PEM)
    output_path.write_bytes(pem_bytes)
    logger.info("CA certificate exported to: %s", output_path)


# ===========================================================================
# HTML → PDF conversion
# ===========================================================================

def html_to_pdf(html_path: Path, pdf_path: Path) -> None:
    """
    Convert an HTML file to PDF using WeasyPrint.

    Args:
        html_path: Path to the source HTML file.
        pdf_path:  Destination path for the generated PDF.

    Raises:
        FileNotFoundError: If the HTML file does not exist.
        RuntimeError:      If WeasyPrint fails to produce output.
    """
    if not html_path.is_file():
        raise FileNotFoundError(f"HTML file not found: {html_path}")

    logger.info("Converting HTML → PDF: %s → %s", html_path, pdf_path)
    HTML(filename=str(html_path)).write_pdf(str(pdf_path))

    if not pdf_path.is_file() or pdf_path.stat().st_size == 0:
        raise RuntimeError("WeasyPrint produced an empty or missing PDF file.")

    logger.info("PDF generated successfully (%d bytes).", pdf_path.stat().st_size)


# ===========================================================================
# PDF signing
# ===========================================================================


def _build_simple_signer(private_key, cert, chain=None) -> signers.SimpleSigner:
    p12_bytes = pkcs12.serialize_key_and_certificates(
        name=b"signer",
        key=private_key,
        cert=cert,
        cas=chain or [],
        encryption_algorithm=serialization.NoEncryption(),
    )
    with tempfile.NamedTemporaryFile(suffix=".p12", delete=False) as tmp:
        tmp.write(p12_bytes)
        tmp_path = tmp.name

    try:
        return signers.SimpleSigner.load_pkcs12(
            pfx_file=tmp_path,
            passphrase=None,
        )
    finally:
        os.unlink(tmp_path)  # Clean up temp file regardless of success/failure

def load_pfx_certificate(pfx_path: Path, passphrase: bytes = None):
    """
    Load a private key and certificate from a .pfx / .p12 file.

    Args:
        pfx_path:   Path to the PKCS#12 file.
        passphrase: Optional passphrase bytes (None if unprotected).

    Returns:
        tuple: (private_key, certificate, ca_chain_list)

    Raises:
        FileNotFoundError: If the PFX file does not exist.
        ValueError:        If the file cannot be parsed.
    """
    if not pfx_path.is_file():
        raise FileNotFoundError(f"Certificate file not found: {pfx_path}")

    logger.info("Loading certificate from: %s", pfx_path)
    pfx_data = pfx_path.read_bytes()

    try:
        private_key, certificate, additional_certs = pkcs12.load_key_and_certificates(
            pfx_data, passphrase
        )
    except Exception as exc:
        raise ValueError(f"Failed to parse PKCS#12 file '{pfx_path}': {exc}") from exc

    if certificate is None:
        raise ValueError("No certificate found inside the PKCS#12 file.")
    if private_key is None:
        raise ValueError("No private key found inside the PKCS#12 file.")

    logger.info(
        "Certificate loaded (subject: %s).",
        certificate.subject.rfc4514_string(),
    )
    return private_key, certificate, list(additional_certs or [])


def sign_pdf(
    unsigned_pdf_path: Path,
    signed_pdf_path: Path,
    private_key,
    cert,
    chain=None,
) -> None:
    """
    Digitally sign a PDF file using pyHanko.

    A visible/invisible signature field is added (or reused if already present),
    and the document is signed with the provided key/certificate.

    Args:
        unsigned_pdf_path: Path to the input (unsigned) PDF.
        signed_pdf_path:   Path where the signed PDF will be written.
        private_key:       Signer's RSA private key.
        cert:              Signer's x509 certificate.
        chain:             Optional CA chain list for embedding in the signature.

    Raises:
        RuntimeError: If signing fails.
    """
    logger.info("Signing PDF: %s → %s", unsigned_pdf_path, signed_pdf_path)

    signer = _build_simple_signer(private_key, cert, chain)

    # Signature metadata embedded in the PDF
    sig_meta = PdfSignatureMetadata(
        field_name=SIG_FIELD_NAME,
        reason=SIG_REASON,
        location=SIG_LOCATION,
        contact_info=SIG_CONTACT,
    )

    with open(unsigned_pdf_path, "rb") as in_fh, \
         open(signed_pdf_path, "wb") as out_fh:

        pdf_writer = IncrementalPdfFileWriter(in_fh)

        # Add a signature field if one with SIG_FIELD_NAME doesn't exist yet.
        # append=True keeps the original content intact (incremental update).
        fields.append_signature_field(
            pdf_writer,
            sig_field_spec=SigFieldSpec(
                sig_field_name=SIG_FIELD_NAME,
                on_page=0,          # First page
                box=(10, 10, 200, 50),  # (x1, y1, x2, y2) in PDF user units
            ),
        )

        signers.sign_pdf(
            pdf_writer,
            signature_meta=sig_meta,
            signer=signer,
            output=out_fh,
        )

    logger.info(
        "PDF signed successfully (%d bytes).", signed_pdf_path.stat().st_size
    )


# ===========================================================================
# CLI argument parsing
# ===========================================================================

def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Convert an HTML file to a digitally signed PDF.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Auto-generate a self-signed CA and sign the PDF:
  python script.py report.html

  # Use an existing certificate:
  python script.py report.html --cert signer.pfx --passphrase secret
        """,
    )
    parser.add_argument(
        "html_file",
        type=Path,
        help="Path to the input HTML file.",
    )
    parser.add_argument(
        "--cert",
        type=Path,
        default=None,
        metavar="CERT_FILE",
        help="Path to a .pfx or .p12 certificate file (optional).",
    )
    parser.add_argument(
        "--passphrase",
        type=str,
        default=None,
        metavar="PASSPHRASE",
        help="Passphrase for the .pfx/.p12 file (if password-protected).",
    )
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=None,
        metavar="DIR",
        help="Directory for output files (defaults to the HTML file's directory).",
    )
    return parser.parse_args()


# ===========================================================================
# Main entry point
# ===========================================================================

def main() -> None:
    args = parse_args()

    html_path: Path = args.html_file.resolve()
    output_dir: Path = (args.output_dir or html_path.parent).resolve()
    output_dir.mkdir(parents=True, exist_ok=True)

    # Derive output file paths from the HTML file's stem
    base_name        = html_path.stem
    unsigned_pdf     = output_dir / f"{base_name}.pdf"
    signed_pdf       = output_dir / f"{base_name}{OUTPUT_SIGNED_SUFFIX}.pdf"
    ca_cert_out      = output_dir / OUTPUT_CA_CERT_FILE

    self_signed_mode = args.cert is None  # True → generate our own CA + cert

    # ------------------------------------------------------------------
    # Step 1: HTML → PDF
    # ------------------------------------------------------------------
    try:
        html_to_pdf(html_path, unsigned_pdf)
    except FileNotFoundError as exc:
        logger.error("Input file error: %s", exc)
        sys.exit(1)
    except Exception as exc:
        logger.error("HTML to PDF conversion failed: %s", exc)
        sys.exit(1)

    # ------------------------------------------------------------------
    # Step 2: Obtain signing credentials
    # ------------------------------------------------------------------
    ca_cert = None  # Only set in self-signed mode

    if self_signed_mode:
        # Generate Root CA, then issue an end-entity cert from it
        try:
            ca_key, ca_cert = generate_self_signed_ca()
            private_key, cert = generate_end_entity_cert(ca_key, ca_cert)
            chain = [ca_cert]  # Embed CA cert in the PDF signature
        except Exception as exc:
            logger.error("Certificate generation failed: %s", exc)
            sys.exit(1)
    else:
        # Load the user-supplied .pfx / .p12 file
        passphrase = (
            args.passphrase.encode() if args.passphrase else None
        )
        try:
            private_key, cert, chain = load_pfx_certificate(args.cert, passphrase)
        except (FileNotFoundError, ValueError) as exc:
            logger.error("Certificate loading failed: %s", exc)
            sys.exit(1)
        except Exception as exc:
            logger.error("Unexpected error loading certificate: %s", exc)
            sys.exit(1)

    # ------------------------------------------------------------------
    # Step 3: Sign the PDF
    # ------------------------------------------------------------------
    try:
        sign_pdf(unsigned_pdf, signed_pdf, private_key, cert, chain)
    except Exception as exc:
        logger.error("PDF signing failed: %s", exc)
        sys.exit(1)

    # ------------------------------------------------------------------
    # Step 4: Export CA certificate (self-signed mode only)
    # ------------------------------------------------------------------
    if self_signed_mode and ca_cert is not None:
        try:
            export_ca_cert_pem(ca_cert, ca_cert_out)
        except Exception as exc:
            logger.error("Failed to export CA certificate: %s", exc)
            sys.exit(1)

    # ------------------------------------------------------------------
    # Final summary
    # ------------------------------------------------------------------
    logger.info("=" * 60)
    logger.info("Output summary:")
    logger.info("  Signed PDF : %s", signed_pdf)

    if self_signed_mode:
        logger.info("  CA Cert    : %s", ca_cert_out)
        logger.info("")
        logger.warning(
            "IMPORTANT: A self-signed CA was generated. "
            "To verify the PDF signature, the verifier must import '%s' "
            "as a trusted root certificate authority.",
            ca_cert_out.name,
        )
    else:
        logger.info("  Certificate: %s (provided by user)", args.cert)

    logger.info("=" * 60)


if __name__ == "__main__":
    main()