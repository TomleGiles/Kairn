// Contenu éditorial du site vitrine. Chaque affirmation correspond à une
// fonctionnalité réelle du produit (voir CLAUDE.md et docs/) : pas de chiffre
// ni de référence client inventés.

export const sources = [
  "OVHcloud",
  "Scaleway",
  "3DS OUTSCALE",
  "OpenStack",
  "Kubernetes",
  "Prometheus",
  "VictoriaMetrics",
  "AWS · Azure · GCP (FOCUS)",
];

export const questions = [
  {
    kicker: "Combien ça coûte ?",
    title: "Le coût réel, alloué et prévu",
    text: "Grilles publiques importées chaque jour, factures réelles rapprochées de l'estimation, coûts on-prem : chaque montant est traçable jusqu'à sa source et attribué à la bonne équipe.",
  },
  {
    kicker: "Est-ce bien utilisé ?",
    title: "Le coût croisé avec l'usage",
    text: "CPU, mémoire, stockage et réseau rapprochés de ce que vous payez, de la VM au pod. Les ressources surdimensionnées, orphelines ou allumées la nuit ressortent d'elles-mêmes.",
  },
  {
    kicker: "Pourquoi ça a bougé ?",
    title: "Les variations expliquées",
    text: "Une dérive est automatiquement corrélée aux déploiements, changements d'inventaire, événements d'autoscaling et incidents de la même fenêtre, puis expliquée preuves à l'appui.",
  },
];

export type Feature = { icon: IconName; title: string; text: string };
export type IconName =
  | "layers"
  | "wand"
  | "pulse"
  | "target"
  | "cube"
  | "server"
  | "chat"
  | "report"
  | "shield"
  | "plug"
  | "globe"
  | "lock"
  | "users"
  | "code";

export const features: Feature[] = [
  {
    icon: "layers",
    title: "Allocation, showback et chargeback",
    text: "Hiérarchie business unit → équipe → service → environnement, règles par labels, projets ou namespaces, coûts partagés répartis. Taux de couverture affiché.",
  },
  {
    icon: "wand",
    title: "Recommandations prêtes à appliquer",
    text: "Rightsizing VM et Kubernetes, orphelins, arrêts hors heures ouvrées, changement de gamme. Économie estimée, risque, preuves et commande CLI, manifeste ou Terraform fournis.",
  },
  {
    icon: "pulse",
    title: "Anomalies corrélées",
    text: "Détection saisonnière par série, corrélation avec GitLab, GitHub, Argo CD, Flux, Alertmanager, PagerDuty… et explication générée à partir des données.",
  },
  {
    icon: "target",
    title: "Budgets et prévisions",
    text: "Budgets à tous les niveaux, alertes sur le réel et sur la prévision de fin de période, simulations what-if : ajout de nodes, changement de gamme, migration de cloud.",
  },
  {
    icon: "cube",
    title: "Kubernetes, au pod près",
    text: "Coût des nodes réparti heure par heure selon max(requests, usage), coût idle rendu visible, plan de contrôle et coûts partagés intégrés.",
  },
  {
    icon: "server",
    title: "On-prem et hybride",
    text: "Matériel amorti, énergie, licences et main-d'œuvre ventilés en coût par vCPU, Go de RAM et Go de stockage. Un agent léger pour les serveurs sans supervision.",
  },
  {
    icon: "chat",
    title: "Assistant IA ancré dans vos données",
    text: "Posez vos questions en langage naturel. Chaque chiffre cité provient des données, avec ses sources ; l'assistant n'a jamais plus de droits que vous.",
  },
  {
    icon: "report",
    title: "Rapport mensuel exécutif",
    text: "Dépense, évolution, variations expliquées, économies réalisées et trois actions prioritaires, en langage non technique. Envoyé en PDF chaque mois.",
  },
];

export const personas = [
  {
    role: "DevOps et SRE",
    need: "Comprendre l'usage, dimensionner, diagnostiquer une dérive.",
    points: ["Coût × usage de la VM au pod", "Anomalies reliées aux déploiements", "CLI, API et serveur MCP"],
  },
  {
    role: "CTO et responsables plateforme",
    need: "Allouer les coûts par équipe et piloter le budget.",
    points: ["Allocation et coûts partagés", "Budgets et prévisions", "Configuration as code (Terraform)"],
  },
  {
    role: "Directions financières",
    need: "Savoir combien, pourquoi, et quoi faire.",
    points: ["Rapport mensuel exécutif", "Chargeback exportable", "Écart estimé / facturé suivi"],
  },
  {
    role: "ESN et infogéreurs",
    need: "Gérer plusieurs clients et refacturer.",
    points: ["Mode multi-clients", "Rapports en marque blanche", "Chargeback par client"],
  },
];

export const steps = [
  {
    n: "01",
    title: "Connectez en lecture seule",
    text: "Identifiants d'application, compte de service ou clé API en lecture seule : chaque connecteur documente les permissions minimales. Objectif : premier cloud connecté en moins de 10 minutes.",
  },
  {
    n: "02",
    title: "Visualisez vos coûts",
    text: "Kairn importe jusqu'à 13 mois d'historique quand la source le permet, tarifie, alloue et croise avec l'usage. Objectif : premiers coûts visibles en moins d'une heure.",
  },
  {
    n: "03",
    title: "Agissez et mesurez",
    text: "Acceptez une recommandation, appliquez la commande fournie : Kairn mesure l'économie réellement obtenue après application.",
  },
];

export const sovereignty = [
  { icon: "globe" as IconName, title: "Hébergé dans l'UE", text: "SaaS opéré chez un hébergeur européen. Aucune donnée client hors UE, sauf appel à un modèle d'IA explicitement autorisé par votre organisation." },
  { icon: "server" as IconName, title: "Édition self-hosted complète", text: "Le même produit, déployé chez vous par chart Helm. Aucun appel sortant obligatoire ; trajectoire SecNumCloud visée." },
  { icon: "chat" as IconName, title: "IA souveraine au choix", text: "Anthropic, Mistral (option souveraine) ou modèle local compatible OpenAI. Ou pas d'IA du tout : c'est votre organisation qui décide." },
  { icon: "lock" as IconName, title: "Lecture seule, toujours", text: "Kairn n'écrit jamais sur votre infrastructure. Credentials chiffrés par enveloppe (KMS ou Vault), jamais journalisés." },
];

export const security = [
  "Isolation stricte entre organisations (Row-Level Security PostgreSQL + contrôle applicatif, testée sur chaque endpoint)",
  "RBAC (Owner, Admin, Finance, Engineer, Viewer) et périmètres par équipe",
  "SSO OIDC et SAML, MFA, provisionnement SCIM",
  "Journal d'audit complet, exportable et en ajout seul",
  "Montants en décimal exact, traçables jusqu'à la ligne de facture ou la grille tarifaire",
  "RGPD : droit à l'effacement, hébergement UE",
];

export const integrations = [
  { icon: "code" as IconName, title: "API REST", text: "OpenAPI 3.1, jetons scoppés, exports CSV et Parquet, webhooks signés." },
  { icon: "plug" as IconName, title: "Terraform / OpenTofu", text: "Connecteurs, allocation et budgets gérés as code." },
  { icon: "chat" as IconName, title: "Serveur MCP", text: "Vos agents IA interrogent Kairn avec vos droits, rien de plus." },
  { icon: "users" as IconName, title: "Slack, Teams, Mattermost", text: "Alertes, e-mail, PagerDuty et webhooks, avec silences et déduplication." },
];

export const plans = [
  {
    name: "Starter",
    target: "PME, un cloud",
    price: "≈ 99 €",
    unit: "/ mois",
    features: ["Coûts et usage", "Recommandations", "Budgets et alertes", "3 utilisateurs"],
    highlight: false,
  },
  {
    name: "Team",
    target: "Multi-cloud et Kubernetes",
    price: "≈ 1 %",
    unit: "de la dépense suivie, dès 490 €/mois",
    features: ["Tout Starter", "Allocation et chargeback", "Anomalies et assistant IA", "Rapports mensuels", "SSO"],
    highlight: true,
  },
  {
    name: "Enterprise",
    target: "Grands comptes, secteur public",
    price: "Sur devis",
    unit: "",
    features: ["Tout Team", "Édition self-hosted", "SCIM et LLM souverain", "SLA et support dédié"],
    highlight: false,
  },
  {
    name: "MSP",
    target: "ESN et infogéreurs",
    price: "Par client",
    unit: "géré",
    features: ["Multi-clients", "Marque blanche", "Chargeback par client"],
    highlight: false,
  },
];

export const faq = [
  {
    q: "Quels clouds et plateformes Kairn prend-il en charge ?",
    a: "OVHcloud, Scaleway, 3DS OUTSCALE, OpenStack (privé ou public), Kubernetes (managé ou on-prem), Prometheus, VictoriaMetrics et Thanos. AWS, Azure et Google Cloud sont couverts via leurs exports de facturation au format FinOps FOCUS, pour les environnements hybrides.",
  },
  {
    q: "Kairn a-t-il besoin d'accès en écriture à mon infrastructure ?",
    a: "Non. Tous les connecteurs sont en lecture seule et chaque type documente les permissions minimales à accorder. Les recommandations fournissent la commande à appliquer, mais c'est vous qui l'exécutez.",
  },
  {
    q: "Où sont hébergées mes données ?",
    a: "Le service SaaS est hébergé dans l'Union européenne. Aucune donnée ne quitte l'UE, sauf si votre organisation autorise explicitement un modèle d'IA non européen. L'édition self-hosted s'installe entièrement chez vous.",
  },
  {
    q: "Faut-il installer un agent ?",
    a: "Rarement. Kairn consomme ce que vous avez déjà : API des clouds, Gnocchi, Prometheus, API Kubernetes. Un agent léger, sans privilège, existe pour les serveurs sans aucune supervision.",
  },
  {
    q: "Comment les coûts Kubernetes sont-ils calculés ?",
    a: "Le coût de chaque node est réparti heure par heure entre ses pods selon le maximum des requests et de l'usage (méthode configurable). La capacité payée mais inutilisée apparaît comme coût idle, et les coûts partagés sont répartis selon vos règles.",
  },
  {
    q: "L'assistant IA peut-il inventer des chiffres ?",
    a: "Il n'accède aux données qu'au travers d'outils typés, avec vos droits, et chaque nombre de sa réponse est vérifié contre les résultats de ces outils. Les sources sont citées ; un chiffre non vérifié est signalé.",
  },
  {
    q: "Combien de temps pour démarrer ?",
    a: "L'objectif est de connecter un premier cloud en moins de 10 minutes et de voir ses premiers coûts en moins d'une heure, historique compris quand la source le permet.",
  },
];
