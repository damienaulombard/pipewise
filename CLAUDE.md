# pipewise — contexte projet pour Claude Code

Langage : **Go**. Module : `github.com/damienaulombard/pipewise`. Licence : Apache-2.0.

## 1. Le problème qu'on résout

Les équipes font tourner les mêmes jobs CI (build, lint, test, e2e) plusieurs fois pour le
**même commit** : sur la merge request, puis au merge sur `main`, puis à la création d'un tag.
Les pipelines sont souvent des cascades de templates `include:` que personne ne lit en entier,
donc personne ne voit la redondance.

Les outils d'observabilité CI mesurent des durées, des tendances, des tests instables. Aucun ne lit
la **configuration**, ne comprend qu'un job tourne trois fois pour un commit, et ne propose le
correctif. C'est ce que fait pipewise : analyse de configuration et remédiation.

## 2. Principes qu'on défend (la doctrine derrière les règles)

- **Une seule étape valide.** Avec les merged results pipelines et le push direct interdit, la
  pipeline de MR teste exactement le commit qui arrive sur `main`. Re-tester sur `main` ou sur le
  tag ne peut rien révéler de nouveau.
- **Un tag est un nom, pas du code.** La pipeline de tag doit publier et déployer, pas valider.
- **Build once, deploy many** (Humble & Farley ; AWS DevOps Guidance liste "building more than
  once" comme anti-pattern). Compromis accepté : rebuild au tag si le build est strictement
  reproductible (lockfiles, images de base par digest) + smoke test avant deploy.
- **Smoke test** = démarrer l'image poussée, healthcheck, 1-2 requêtes, vérifier la version.
  Attrape les erreurs de packaging, pas de logique métier.
- **Chaque pipeline a un rôle distinct** : MR = valider, main = publier (ou rien), tag = nommer
  et déployer.

Références : docs.gitlab.com/ci/pipelines/pipeline_efficiency/, docs.gitlab.com/ee/ci/components/,
continuousdelivery.com, AWS Well-Architected DevOps Guidance (anti-patterns for CD),
Software Engineering at Google ch. 23, rapports DORA (lead time for changes).

## 3. Périmètre v0 (ce qu'on construit maintenant)

**GitLab uniquement, analyse statique, aucun service.**

Commandes :
- `pipewise lint <fichier.yml>` — analyse un `.gitlab-ci.yml` ou un YAML déjà fusionné.
- `pipewise fetch --project <group/project> [--ref main]` — récupère le YAML fusionné via
  `GET /projects/:id/ci/lint?include_merged_yaml=true` (token via `GITLAB_TOKEN`,
  URL via `GITLAB_URL`, défaut `https://gitlab.com`). Écrit sur stdout ou `-o`.
- `pipewise lint --format json` pour l'intégration. Défaut : rapport texte lisible.
- Code de sortie non nul si un finding de sévérité ≥ `--fail-on` (défaut : `error`).

Cœur : **évaluateur de `rules`**. Pour chaque job, simuler trois événements avec les
variables prédéfinies GitLab correspondantes et répondre "ce job tourne-t-il ?" :
- `merge_request_event` (`CI_PIPELINE_SOURCE=merge_request_event`, `CI_MERGE_REQUEST_IID` défini,
  `CI_COMMIT_BRANCH` non défini)
- push sur la branche par défaut (`CI_PIPELINE_SOURCE=push`, `CI_COMMIT_BRANCH=main`,
  `CI_DEFAULT_BRANCH=main`)
- tag (`CI_PIPELINE_SOURCE=push`, `CI_COMMIT_TAG=v1.0.0`, `CI_COMMIT_BRANCH` non défini)

Supporter : `rules` (`if`, `when`, `changes` → traité comme "peut-être", `exists` idem,
`allow_failure`), `workflow: rules`, `only`/`except` (forme simple + `refs`), `extends`
(héritage résolu avant évaluation), les anchors YAML. Sémantique `if` : `==`, `!=`, `=~`,
`!~`, `&&`, `||`, parenthèses, variables définies / non définies, `null`.
Résultat par job et par événement : `runs` / `skipped` / `manual` / `maybe` (dépend de `changes`
ou d'une variable inconnue).

Règles v0 (chacune : id stable, sévérité, message, explication, suggestion de correctif) :
1. `redundant-job` (error) — job qui tourne sur MR **et** main et/ou tag. Message explicite :
   "tourne N fois pour un même commit". Suggestion : `rules` restreignant à un seul événement.
2. `no-rules` (warning) — job sans `rules`/`only`/`except`, donc partout par défaut.
3. `missing-interruptible` (info) — job qui tourne sur MR sans `interruptible: true`.
4. `floating-image` (warning) — `image:` en `latest`, sans tag, ou sans `@sha256:` digest.
5. `artifacts-no-expiry` (info) — `artifacts:` sans `expire_in`.

Hors périmètre v0 : GitHub Actions, génération de patches, webhooks, UI, analyse dynamique,
`trigger:` / child pipelines (les ignorer proprement, pas planter), `include:` non résolus
(le CLI travaille sur du YAML fusionné ; si des `include:` restent, l'indiquer et continuer).

## 4. Architecture Go

```
pipewise/
├── cmd/pipewise/main.go          # cobra : lint, fetch, version
├── internal/
│   ├── config/                   # parse YAML → modèle typé (Pipeline, Job, Rule…)
│   │   ├── parse.go
│   │   ├── extends.go            # résolution extends + anchors
│   │   └── model.go
│   ├── eval/                     # évaluateur de rules
│   │   ├── context.go            # les 3 événements et leurs variables
│   │   ├── expr.go               # parseur/évaluateur d'expressions `if`
│   │   ├── rules.go              # applique rules/only/except/workflow → verdict
│   │   └── eval_test.go
│   ├── rules/                    # une règle = un fichier
│   │   ├── rule.go               # interface Rule { ID, Severity, Check(*Analysis) []Finding }
│   │   ├── redundant_job.go
│   │   ├── no_rules.go
│   │   ├── missing_interruptible.go
│   │   ├── floating_image.go
│   │   └── artifacts_no_expiry.go
│   ├── report/                   # text + json
│   └── gitlab/                   # client API minimal (fetch merged yaml)
├── testdata/                     # fixtures YAML, une par cas (+ golden files pour report)
├── go.mod
├── Makefile                      # build, test, cover, lint, fmt, check
└── README.md
```

Dépendances : `gopkg.in/yaml.v3`, `github.com/spf13/cobra`. Rien d'autre sans raison.
Tests : table-driven, fixtures dans `testdata/`, golden files pour la sortie texte.
**100 % de couverture sur `internal/eval`** : c'est le composant qui conditionne tout le reste.
Les règles de `internal/rules` ne commencent pas avant.

La sortie `--format json` et les ids de règles sont l'API publique du CLI : un outil qui
consomme pipewise s'appuie dessus. Ils ne changent pas sans version majeure.

## 5. Conventions

- Go 1.26+, `golangci-lint` v2 propre (`make lint`), formatage par `make fmt`.
- Pas de `panic` sur une config invalide : finding de type `parse-error` et on continue.
- Les messages de findings s'adressent à un dev qui n'a pas lu cette doctrine : une phrase
  de constat, une phrase de conséquence, une suggestion concrète en YAML.
- Les ids de règles sont stables et kebab-case ; ils font partie de l'API publique.
- Commits Conventional Commits **avec scope** : `feat(eval): …`, `fix(config): …`,
  `chore(repo): …`. Une PR par issue, merge en squash.
- Tout en anglais dans le code, les messages du CLI, le README, les issues et les PR ;
  ce fichier reste en français.
- La CI ne tourne que sur les pull requests : on applique notre propre doctrine.
- Aucune attribution d'outil dans les commits, PR et issues : pas de `Co-Authored-By` ni de mention
  « Generated with … ».

## 6. Roadmap après v0 (pour mémoire, ne pas anticiper dans le code)

- v0.1 : `pipewise explain <job>` — montre pour chaque événement pourquoi le job tourne ou non
  (la rule qui a matché). Très utile pour la confiance dans l'évaluateur.
- v0.2 : génération de patch (`--fix` → diff unifié) pour `missing-interruptible` et
  `artifacts-no-expiry` (sans risque), puis `redundant-job` avec préconditions vérifiées
  via l'API (merged results activé, push direct interdit sur la branche par défaut).
- v0.3 : règles supplémentaires : `e2e-everywhere`, `stages-without-needs`,
  `version-baked-at-build` (usage de `$CI_COMMIT_TAG` dans un `build`), `no-cache`.
- v1 : GitHub Actions (résolution des `uses:` et reusable workflows, évaluateur d'expressions
  `${{ }}`).
