#!/usr/bin/env python3
"""Generates docs/images/architecture.svg: bluepave's architecture with the real product logos.

The logos come from their official sources (Microsoft's Azure architecture icons, the CNCF
artwork repository, and each project's own repository), are cached in a temporary folder, and are
embedded in the SVG as data URIs, so the diagram is one self-contained file that GitHub renders.
kro publishes no logo file, so it gets a text badge.

    python3 hack/architecture-diagram/generate.py [cache dir]
"""

import base64
import io
import os
import sys
import tempfile
import urllib.request
import zipfile
from xml.sax.saxutils import escape

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
OUT = os.path.join(ROOT, "docs", "images", "architecture.svg")

AZURE_ICONS = "https://arch-center.azureedge.net/icons/Azure_Public_Service_Icons_V23.zip"
AZURE = {  # name -> file in the zip (Azure_Public_Service_Icons/Icons/...)
    "aks": "compute/10023-icon-service-Kubernetes-Services.svg",
    "acr": "containers/10105-icon-service-Container-Registries.svg",
    "keyvault": "security/10245-icon-service-Key-Vaults.svg",
    "postgres": "databases/10131-icon-service-Azure-Database-PostgreSQL-Server.svg",
    "dns": "networking/10064-icon-service-DNS-Zones.svg",
    "vnet": "networking/10061-icon-service-Virtual-Networks.svg",
    "identity": "identity/10227-icon-service-Managed-Identities.svg",
    "entra": "identity/10232-icon-service-App-Registrations.svg",
    "loganalytics": "analytics/00009-icon-service-Log-Analytics-Workspaces.svg",
    "monitor": "management + governance/00001-icon-service-Monitor.svg",
    "storage": "storage/10086-icon-service-Storage-Accounts.svg",
    "policy": "management + governance/10316-icon-service-Policy.svg",
    "cost": "general/10019-icon-service-Cost-Management.svg",
    "subscription": "general/10002-icon-service-Subscriptions.svg",
    "users": "identity/10230-icon-service-Users.svg",
}
CNCF = "https://raw.githubusercontent.com/cncf/artwork/main/projects/{0}/icon/color/{0}-icon-color.svg"
OSS = {
    "kubernetes": CNCF.format("kubernetes"),
    "argo": CNCF.format("argo"),
    "kyverno": CNCF.format("kyverno"),
    "falco": CNCF.format("falco"),
    "envoy": CNCF.format("envoy"),
    "certmanager": CNCF.format("cert-manager"),
    "backstage": CNCF.format("backstage"),
    "otel": CNCF.format("opentelemetry"),
    "helm": CNCF.format("helm"),
    "prometheus": CNCF.format("prometheus"),
    "kargo": "https://raw.githubusercontent.com/akuity/kargo/main/ui/public/kargo-logo-head-only.png",
    "grafana": "https://raw.githubusercontent.com/grafana/grafana/main/packages/grafana-ui/src/components/PageLoader/grafana_icon.svg",
    "tempo": "https://raw.githubusercontent.com/grafana/tempo/main/cmd/tempo/app/static/tempo-icon.png",
    "cosign": "https://raw.githubusercontent.com/sigstore/community/main/artwork/cosign/icons/color/sigstore_cosign-icon-color.svg",
    "eso": "https://raw.githubusercontent.com/external-secrets/external-secrets/main/assets/eso-icon.svg",
    "aso": "https://raw.githubusercontent.com/Azure/azure-service-operator/main/docs/hugo/assets/icons/logo.svg",
    "github": "https://raw.githubusercontent.com/primer/octicons/main/icons/mark-github-24.svg",
    "actions": "https://raw.githubusercontent.com/github/explore/main/topics/actions/actions.png",
    "trivy": "https://raw.githubusercontent.com/aquasecurity/trivy/main/brand/Trivy-OSS-Logo-Color-Stacked-RGB.svg",
}


def fetch(url, path):
    if not os.path.exists(path):
        with urllib.request.urlopen(url, timeout=60) as r, open(path, "wb") as f:
            f.write(r.read())
    with open(path, "rb") as f:
        return f.read()


def load_logos(cache):
    os.makedirs(cache, exist_ok=True)
    logos = {}
    z = zipfile.ZipFile(io.BytesIO(fetch(AZURE_ICONS, os.path.join(cache, "azure-icons.zip"))))
    for name, member in AZURE.items():
        logos[name] = ("image/svg+xml", z.read("Azure_Public_Service_Icons/Icons/" + member))
    for name, url in OSS.items():
        data = fetch(url, os.path.join(cache, name + os.path.splitext(url)[1]))
        logos[name] = ("image/png" if url.endswith(".png") else "image/svg+xml", data)
    return {k: "data:%s;base64,%s" % (t, base64.b64encode(d).decode()) for k, (t, d) in logos.items()}


# --- drawing -----------------------------------------------------------------------------------

FONT = "'Segoe UI', -apple-system, 'Helvetica Neue', Arial, sans-serif"
INK, MUTED = "#1f2328", "#57606a"


class Svg:
    def __init__(self, w, h, logos):
        self.w, self.h, self.logos, self.parts = w, h, logos, []

    def add(self, s):
        self.parts.append(s)

    def text(self, x, y, s, size=13, weight=400, color=INK, anchor="start"):
        self.add(f'<text x="{x}" y="{y}" font-size="{size}" font-weight="{weight}" fill="{color}" '
                 f'text-anchor="{anchor}">{escape(s)}</text>')

    def logo(self, name, x, y, size):
        if name == "kro":  # no published logo file: a badge in kro's blue
            self.add(f'<rect x="{x}" y="{y + size * 0.2}" width="{size}" height="{size * 0.6}" rx="4" fill="#326ce5"/>')
            self.text(x + size / 2, y + size * 0.62, "kro", size=size * 0.36, weight=700, color="#fff", anchor="middle")
            return
        if name == "eso":  # drawn in white for dark backgrounds
            self.add(f'<rect x="{x}" y="{y}" width="{size}" height="{size}" rx="6" fill="#16325c"/>')
            x, y, size = x + size * 0.12, y + size * 0.12, size * 0.76
        self.add(f'<image x="{x}" y="{y}" width="{size}" height="{size}" href="{self.logos[name]}" '
                 f'preserveAspectRatio="xMidYMid meet"/>')

    def group(self, x, y, w, h, title, fill, stroke, logo=None, dash=False, note=None):
        d = ' stroke-dasharray="6 4"' if dash else ""
        self.add(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="12" fill="{fill}" stroke="{stroke}" stroke-width="1.5"{d}/>')
        tx = x + 14
        if logo:
            self.logo(logo, x + 12, y + 9, 26)
            tx = x + 46
        self.text(tx, y + 28, title, size=15, weight=600)
        if note:
            self.text(x + w - 14, y + 28, note, size=12, color=MUTED, anchor="end")

    def card(self, x, y, w, h, logo, title, sub=None, logo2=None):
        self.add(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="8" fill="#ffffff" stroke="#d0d7de"/>')
        size = min(h - 18, 40)
        self.logo(logo, x + 10, y + (h - size) / 2, size)
        tx = x + 18 + size
        if logo2:
            self.logo(logo2, x + w - 34, y + 8, 24)
        if sub:
            self.text(tx, y + h / 2 - 3, title, size=13, weight=600)
            for i, line in enumerate(sub.split("\n")):
                self.text(tx, y + h / 2 + 14 + i * 14, line, size=11, color=MUTED)
        else:
            self.text(tx, y + h / 2 + 5, title, size=13, weight=600)

    def arrow(self, points, color="#0969da", dash=False, badge=None, badge_at=None):
        d = "M " + " L ".join(f"{x} {y}" for x, y in points)
        dd = ' stroke-dasharray="5 4"' if dash else ""
        self.add(f'<path d="{d}" fill="none" stroke="{color}" stroke-width="2"{dd} marker-end="url(#arrow-{color[1:]})"/>')
        if badge:
            bx, by = badge_at or points[len(points) // 2]
            self.add(f'<circle cx="{bx}" cy="{by}" r="11" fill="{color}"/>')
            self.text(bx, by + 4.5, badge, size=12, weight=700, color="#fff", anchor="middle")

    def render(self, colors):
        markers = "".join(
            f'<marker id="arrow-{c[1:]}" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" '
            f'orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="{c}"/></marker>' for c in colors)
        return (f'<svg xmlns="http://www.w3.org/2000/svg" width="{self.w}" height="{self.h}" '
                f'viewBox="0 0 {self.w} {self.h}" font-family="{FONT}">\n'
                f'<defs>{markers}</defs>\n<rect width="100%" height="100%" fill="#ffffff"/>\n'
                + "\n".join(self.parts) + "\n</svg>\n")


def draw(logos):
    s = Svg(1900, 1210, logos)
    BLUE, GREEN, PURPLE, ORANGE = "#0969da", "#1a7f37", "#8250df", "#bc4c00"

    s.text(30, 44, "bluepave architecture", size=24, weight=700)
    s.text(30, 68, "One configuration file and one CLI create the Azure resources; from then on Git is the "
                   "source of truth and Argo CD runs the platform.", size=13, color=MUTED)

    # --- GitHub -------------------------------------------------------------------------------
    s.group(30, 90, 330, 620, "GitHub", "#f6f8fa", "#d0d7de", logo="github")
    s.card(50, 140, 290, 92, "github", "Platform repository",
           "bluepave.yaml, modules/, apps/*.yaml,\nservices/<app>/ (code, chart, values)")
    s.group(50, 252, 290, 230, "Golden path (build-app.yml)", "#ffffff", "#d0d7de", logo="actions")
    s.card(64, 296, 262, 54, "actions", "Checks", "tests, Semgrep, gitleaks, govulncheck")
    s.card(64, 358, 262, 54, "trivy", "Build and scan", "image + SBOM + SLSA provenance")
    s.card(64, 420, 262, 54, "cosign", "Sign (Cosign keyless)", "identity: build-app.yml @ main")
    s.card(50, 502, 290, 76, "github", "Platform GitHub App",
           "Argo CD reads, Kargo commits,\nthe portal opens pull requests")
    s.card(50, 598, 290, 92, "entra", "CI identity (OIDC)",
           "GitHub environments trusted by Entra:\nno secrets in GitHub")

    # People
    s.group(30, 740, 330, 320, "People", "#f6f8fa", "#d0d7de")
    s.card(50, 786, 290, 76, "users", "App team",
           "pull request: code, chart, apps/<app>.yaml\n(or a portal template)")
    s.card(50, 876, 290, 76, "users", "Platform engineer",
           "bluepave up / status / down\n(an Owner of the subscription)")
    s.card(50, 966, 290, 76, "users", "Users", "https://<app>.<env>.<domain>")

    # --- Azure --------------------------------------------------------------------------------
    s.group(390, 90, 1480, 970, "Azure subscription", "#f0f6ff", "#0969da", logo="subscription",
            note="Bicep, one deployment stack per module and environment")
    s.group(410, 140, 1440, 108, "Subscription-wide", "#ffffff", "#9ec5fe", dash=True)
    for i, (lg, t, sub) in enumerate([
        ("dns", "Azure DNS", "<domain>, <env>.<domain>"),
        ("entra", "Microsoft Entra ID", "admins group, app sign-in"),
        ("identity", "Managed identities", "Workload Identity, no secrets"),
        ("policy", "Azure Policy", "allowed regions, tags"),
        ("cost", "Cost Management", "budget per environment"),
    ]):
        s.card(430 + i * 282, 172, 266, 64, lg, t, sub)

    s.group(410, 266, 1440, 780, "Environment: dev", "#ffffff", "#9ec5fe", dash=True,
            note="rg-<prefix>-dev-<region>-*  (prod is its own environment)")
    azure_res = [
        ("vnet", "Virtual network", "hub-spoke, private endpoints"),
        ("acr", "Container Registry", "per-app push and pull (ABAC)"),
        ("keyvault", "Key Vault", "RBAC only; secrets for add-ons"),
        ("postgres", "PostgreSQL", "Entra ID only, private endpoint"),
        ("storage", "Storage accounts", "AppStorage: keyless"),
        ("monitor", "Azure Monitor", "managed Prometheus"),
        ("loganalytics", "Log Analytics", "AKS and platform logs"),
    ]
    for i, (lg, t, sub) in enumerate(azure_res):
        s.card(430, 306 + i * 100, 262, 80, lg, t, sub)

    # --- AKS ------------------------------------------------------------------------------------
    s.group(720, 296, 1110, 734, "AKS cluster", "#f5f0ff", "#8250df", logo="aks",
            note="Azure CNI Overlay + Cilium · Entra ID only · Workload Identity")
    rows = [
        ("GitOps and delivery", [("argo", "Argo CD", "syncs the platform repo"),
                                 ("kargo", "Kargo", "dev → staging promotion"),
                                 ("argo", "Argo Rollouts", "canary releases"),
                                 ("backstage", "Portal (Backstage)", "catalog, templates, docs")]),
        ("Guardrails", [("kyverno", "Kyverno", "only golden-path images"),
                        ("kubernetes", "Pod Security", "restricted, per namespace"),
                        ("kubernetes", "NetworkPolicy", "default deny (Cilium)"),
                        ("falco", "Falco", "runtime detection")]),
        ("Edge", [("envoy", "Envoy Gateway", "Gateway API, HTTPS"),
                  ("certmanager", "cert-manager", "Let's Encrypt, DNS-01"),
                  ("kubernetes", "external-dns", "records in Azure DNS"),
                  ("eso", "External Secrets", "Key Vault → Secrets")]),
        ("Platform APIs", [("kro", "kro", "AppDatabase, AppCache, AppStorage"),
                           ("aso", "Azure Service Operator", "creates the Azure resources"),
                           ("kubernetes", "Valkey (AppCache)", "in the app's namespace"),
                           ("helm", "App onboarding", "namespaces, project, quota")]),
        ("Observability", [("otel", "OpenTelemetry Collector", "OTLP from apps"),
                           ("tempo", "Tempo", "traces, 24 h"),
                           ("grafana", "Grafana", "Entra ID sign-in"),
                           ("prometheus", "Prometheus metrics", "→ Azure Monitor")]),
    ]
    y = 344
    for title, cards in rows:
        s.text(740, y + 4, title.upper(), size=11, weight=700, color=PURPLE)
        for i, (lg, t, sub) in enumerate(cards):
            s.card(740 + i * 270, y + 12, 254, 64, lg, t, sub)
        y += 104

    # App namespaces
    s.group(740, 870, 1070, 140, "App namespaces", "#ffffff", "#8250df", dash=True,
            note="created from apps/<app>.yaml · Pod Security restricted")
    for i, (stage, how) in enumerate([("<app>-dev", "every new image, automatically"),
                                      ("<app>-staging", "promoted by hand in Kargo")]):
        x = 760 + i * 530
        s.card(x, 912, 500, 80, "kubernetes", stage,
               how + "\nsigned images pinned by digest; ConfigMaps from the platform APIs")

    # --- Arrows -----------------------------------------------------------------------------------
    # 1 app team -> repository (pull request), along the left edge
    s.arrow([(50, 824), (40, 824), (40, 186), (50, 186)], color=BLUE, badge="1", badge_at=(40, 520))
    # 2 repository -> golden path (on merge)
    s.arrow([(195, 232), (195, 252)], color=BLUE)
    s.add(f'<circle cx="215" cy="242" r="11" fill="{BLUE}"/>')
    s.text(215, 246.5, "2", size=12, weight=700, color="#fff", anchor="middle")
    # 3 golden path -> Container Registry (push, OIDC)
    s.arrow([(340, 446), (430, 446)], color=BLUE, badge="3", badge_at=(375, 446))
    # 4 Kargo watches the registry: below the first row of cluster cards
    s.arrow([(692, 432), (1137, 432), (1137, 422)], color=ORANGE, dash=True, badge="4", badge_at=(1000, 432))
    # 5 Kargo commits digests to the repository: up through the gap between two subscription-wide
    # cards, along the top of the subscription
    s.arrow([(1250, 356), (1250, 342), (1269, 342), (1269, 131), (370, 131), (370, 170), (340, 170)],
            color=ORANGE, badge="5", badge_at=(800, 131))
    # 6 Argo CD syncs from the repository
    s.arrow([(340, 205), (380, 205), (380, 279), (885, 279), (885, 354)], color=GREEN,
            badge="6", badge_at=(620, 279))
    # 7 Kyverno verifies signatures against the registry
    s.arrow([(740, 492), (706, 492), (706, 466), (694, 466)], color=PURPLE, dash=True, badge="7",
            badge_at=(706, 510))
    # 8 users -> Envoy Gateway
    s.arrow([(340, 1004), (382, 1004), (382, 1052), (710, 1052), (710, 600), (738, 600)], color=GREEN,
            badge="8", badge_at=(560, 1052))
    # 9 platform engineer -> Azure (bluepave up)
    s.arrow([(340, 914), (402, 914)], color=BLUE, badge="9", badge_at=(372, 914))

    # --- Legend -----------------------------------------------------------------------------------
    s.text(30, 1100, "How a change ships", size=15, weight=600)
    steps = [
        ("1", BLUE, "The app team opens a pull request (by hand or from a portal template)."),
        ("2", BLUE, "On merge, the golden path checks, builds and scans the images."),
        ("3", BLUE, "It pushes them to the registry with an SBOM and provenance, signed with Cosign (OIDC, no secrets)."),
        ("4", ORANGE, "Kargo sees the new images in the registry ..."),
        ("5", ORANGE, "... and commits their digests to the stage's values file (dev automatically, later stages on request)."),
        ("6", GREEN, "Argo CD syncs the platform and every app from the repository."),
        ("7", PURPLE, "Kyverno admits only images signed by the golden path, and each app only its own."),
        ("8", GREEN, "Users reach apps through Envoy Gateway, with Let's Encrypt certificates and records in Azure DNS."),
        ("9", BLUE, "The platform engineer runs bluepave up once: Bicep stacks, identities, the GitHub App, then Argo CD."),
    ]
    for i, (n, c, t) in enumerate(steps):
        x = 30 + (i // 3) * 620
        yy = 1128 + (i % 3) * 26
        s.add(f'<circle cx="{x + 10}" cy="{yy - 4}" r="10" fill="{c}"/>')
        s.text(x + 10, yy, n, size=11, weight=700, color="#fff", anchor="middle")
        s.text(x + 28, yy, t, size=12, color=INK)
    return s.render([BLUE, GREEN, PURPLE, ORANGE])


def main():
    cache = sys.argv[1] if len(sys.argv) > 1 else os.path.join(tempfile.gettempdir(), "bluepave-logos")
    svg = draw(load_logos(cache))
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w") as f:
        f.write(svg)
    print(f"wrote {os.path.relpath(OUT, ROOT)} ({len(svg) // 1024} KiB)")


if __name__ == "__main__":
    main()
