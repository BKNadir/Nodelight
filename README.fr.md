<p align="center"><img src="assets/icon.png" alt="Nodelight" width="160"></p>

<p align="center"><a href="README.md">English</a> · <b>Français</b> · <a href="README.nl.md">Nederlands</a> · <a href="README.de.md">Deutsch</a> · <a href="README.es.md">Español</a></p>

# Nodelight

Un client Moonlight **portable pour Windows** qui se connecte à votre PC Sunshine/Apollo **à travers
Tailscale** : pas d'installation, pas de pilote, pas de droits administrateur, et aucun port à ouvrir
sur la box.

> **Projet généré par une IA.** Tout le code, les scripts et la documentation de ce dépôt ont été
> écrits par **Claude Sonnet 5.5** (Anthropic), via Claude Code, sous la direction de l'auteur du
> dépôt (« vibe coding »). Le code n'a **pas été audité par un humain** et n'a été **que partiellement
> testé sur une machine réelle** (il se compile, se lance et s'affiche ; le streaming complet à travers
> Tailscale n'est pas confirmé) : utilisez-le en connaissance de cause (voir « État de vérification »).

Ce dépôt est un **fork de [moonlight-qt](https://github.com/moonlight-stream/moonlight-qt)** : le code du
client est celui de Moonlight, avec les modifications de Nodelight ajoutées en commits par-dessus
(`git log` pour les voir). Le dépôt d'origine est configuré comme `upstream` pour récupérer ses mises à jour.
Le reste de ce fichier décrit uniquement ce que Nodelight ajoute.

Nodelight n'est affilié ni à Tailscale Inc., ni au projet Moonlight, ni à Artemis/Apollo/Sunshine.
Tailscale est une marque de Tailscale Inc.

## Comment ça marche

| Composant | Langage | Rôle |
| --- | --- | --- |
| `bridge/` → `Nodelight.exe` | Go | Nœud Tailscale en espace utilisateur (`tsnet`) + relais TCP/UDP local, lance Moonlight |
| `app/`, `moonlight-common-c/`… → `client\NodelightClient.exe` | C++/Qt | Le client Moonlight modifié : mode « distant » sur le relais, option écran virtuel d'Apollo reprise d'Artemis, page Tailscale intégrée, nom et icône Nodelight |

Moonlight diffuse en UDP (vidéo, audio, contrôle) avec des sockets bruts qui ne passent pas par un
proxy SOCKS5, et il lit les numéros de port dans les réponses RTSP de l'hôte. Nodelight écoute donc
**les mêmes ports que Sunshine** sur une adresse loopback dédiée par PC (`127.77.0.1`,
`127.77.0.2`, …) et les transmet via le tailnet.

```
Moonlight ──► 127.77.0.1:47989/47984/48010 (TCP), :47998-48002 (UDP)
                 │  Nodelight (relais + tsnet/WireGuard, espace utilisateur)
                 ▼
          PC hôte Sunshine/Apollo  (via votre tailnet)
```

Ports relayés par hôte (base 47989) : TCP 47984, 47989, 48010 (+ 47990 avec `--web-ui`),
UDP 47998, 47999, 48000, 48002.

## Utilisation

1. Double-cliquez sur `Nodelight.exe` : **aucune console ne s'ouvre**, Moonlight démarre directement
   (en mode portable, fichier `portable.dat` : réglages et appairages restent dans le dossier).
   Nodelight se ferme avec Moonlight. Ce qu'il aurait affiché est écrit dans `Nodelight.log`, et les
   erreurs de démarrage s'affichent dans une boîte de dialogue.
2. Au premier lancement, Moonlight ouvre sa page **Tailscale** (bouton avec une icône de réseau dans la
   barre du haut). Cliquez sur **Connect** : une page web s'ouvre pour autoriser l'appareil (une seule
   fois). L'identité est stockée dans `data\` (gardez ce dossier privé ; il peut être copié d'un PC à
   l'autre). Aucun PC n'est nécessaire pour se connecter.
3. Une fois connecté, choisissez au besoin le **nœud de sortie** (voir plus bas), puis **Add a PC** :
   un nom et le nom Tailscale ou l'IP du PC, puis **Save PCs**. Le PC est pris en compte immédiatement.
4. Les PC apparaissent **automatiquement** dans Moonlight (`127.77.0.1`, `127.77.0.2`… dans l'ordre de
   la liste, à ne pas réordonner si des PC sont déjà appairés). Appairez comme d'habitude.

Lancé depuis un terminal, `Nodelight.exe` écrit dans ce terminal et pose les questions du premier
lancement (utile avec `--json` ou `--no-launch`).

Options utiles : `--host nom=adresse[:portBase]` (répétable), `--hostname`, `--state-dir`,
`--no-browser`, `--json` (événements ligne par ligne), `--listen-prefix`, `--web-ui`, et
`--control-url` pour un serveur Headscale auto-hébergé, et `--exit-node` (voir ci-dessous).

### Page de configuration

Connexion, nœud de sortie, PC et profils se règlent sans éditer de fichier, de deux façons :

- **Dans Moonlight** : bouton **Tailscale (Nodelight)** (icône de réseau) dans la barre du haut. La page
  contient, avec des commandes cliquables : l'état de la connexion et **Connect / Disconnect** (plus
  un bouton pour ouvrir la page de connexion Tailscale), le **nœud de sortie** (liste de votre tailnet
  + **Apply exit node**), la **liste des PC** (nom, adresse, port de base, adresse locale
  `127.77.0.N`, voyant vert quand le PC répond, **Add a PC** / ✕ / **Save PCs**) et les **profils**
  (changer, supprimer, créer). Le bouton n'apparaît que si Moonlight a été lancé par Nodelight.
  Une section équivalente, plus courte, reste dans Paramètres.
- **Dans le navigateur** (secours) : l'adresse est écrite dans `Nodelight.log`
  (`Settings page: http://127.0.0.1:PORT/?token=…`) ou dans le terminal ; même contenu.

Un **profil** regroupe une identité Tailscale, un nom d'appareil, un serveur de contrôle facultatif
(Headscale) et une liste de PC. C'est ce qui permet de passer d'un réseau Tailscale à un autre ;
chaque profil garde son identité dans `data\profiles\<nom>`, le profil par défaut reste dans `data\`.
Les profils sont enregistrés dans `hosts.json` (les fichiers des versions précédentes restent valides).

Notes :
- Se déconnecter, changer de profil ou de nœud de sortie **interrompt le streaming en cours**.
- Une valeur donnée en ligne de commande (`--exit-node`, `--host`…) s'applique au démarrage sans être
  enregistrée ; un choix fait ensuite dans la page la remplace.
- Sécurité : le serveur n'écoute que sur `127.0.0.1`, chaque appel exige un jeton aléatoire généré à chaque
  lancement (transmis à Moonlight par variable d'environnement), et les requêtes dont l'en-tête `Host`
  n'est pas l'adresse locale sont refusées (protection contre le « DNS rebinding »). Un autre programme
  tournant sous votre session Windows peut cependant lire cet environnement : ce n'est pas une
  frontière de sécurité contre du code déjà exécuté sur votre PC.

### PC hôte qui n'est pas sur Tailscale (exit node)

Si votre PC Sunshine/Apollo n'a pas Tailscale mais qu'une autre machine de son réseau local (Home
Assistant, NAS, Raspberry Pi…) est sur votre tailnet **en tant qu'exit node**, Nodelight peut passer
par elle :

```json
{
  "exitNode": "homeassistant",
  "hosts": [ { "name": "bureau", "address": "192.168.1.20" } ]
}
```

- `exitNode` : nom ou IP Tailscale du nœud de sortie (ou `--exit-node`, ou la liste de la page
  Tailscale). `address` est alors l'**adresse locale** du PC hôte (`192.168.x.y`), vue depuis le
  réseau de l'exit node.
- L'exit node doit être approuvé dans la console d'administration Tailscale (Machines → Edit route
  settings → Use as exit node). L'appareil `nodelight` doit avoir le droit de l'utiliser (c'est le cas
  avec les ACL par défaut).
- Même principe qu'une machine distante qui utilise cet exit node : si votre PC joint l'adresse locale
  de l'hôte ainsi, Nodelight devrait le faire aussi. **Non testé** : je n'ai pas pu vérifier que
  la bibliothèque `tsnet` achemine bien le trafic UDP du streaming vers une adresse locale par un exit
  node. C'est le premier point à tester.
- Performances : tout le flux vidéo transite par la machine exit node, en chiffrement WireGuard. Un
  petit serveur domotique peut limiter le débit ; baissez le débit vidéo dans Moonlight si l'image
  saccade. Si l'hôte peut recevoir Tailscale, c'est toujours plus simple et plus rapide (connexion
  directe, adresse `100.x` ou nom MagicDNS à la place de l'IP locale et plus d'exit node).

### Côté hôte (Sunshine/Apollo)

- Le PC hôte doit être dans le même tailnet et allumé, avec Sunshine/Apollo démarré. Nodelight
  indique au démarrage si chaque hôte répond sur son port HTTP.
- Le pare-feu Windows de l'hôte doit autoriser Sunshine/Apollo sur l'interface Tailscale
  (profil réseau concerné) ; vos ACL Tailscale doivent autoriser ces ports.
- Sunshine n'autorise par défaut l'appairage (`origin_pin_allowed`) que depuis le réseau local :
  si l'appairage est refusé, ajustez ce réglage ou appairez une première fois sur le LAN.
- Pour de bonnes performances, il faut une connexion Tailscale **directe** (pas via un relais DERP) ;
  le chiffrement WireGuard en espace utilisateur consomme un peu de CPU.

## Obtenir le build sans environnement de développement

Le binaire se construit sur les machines Windows gratuites de GitHub Actions :

1. Onglet **Actions** → **Build Windows x64 (portable)** → **Run workflow** (30 à 60 minutes ;
   un `push` sur `master` le lance aussi).
   Les anciens workflows de Moonlight (AppImage, Steam Link, signature Windows/macOS) ont été retirés
   de ce fork : ils demandent des secrets qui n'existent pas ici.
2. Téléchargez l'artefact `Nodelight-win-x64` (un zip), décompressez-le : c'est tout.

Le zip contient le runtime Visual C++ (copie locale) : aucun prérequis sur la machine cible.
Aucune élévation de privilèges n'est demandée (manifeste `asInvoker`, pont en espace utilisateur,
ports locaux > 1024 uniquement sur loopback). L'exécutable n'est pas signé : SmartScreen peut
demander une confirmation, et le pare-feu Windows peut proposer une autorisation réseau que vous
pouvez annuler.

## Compilation manuelle (Windows x64)

Prérequis : Go 1.26+, Git, Qt 6.11+ (MSVC), Visual Studio 2026 (ou la version requise par
moonlight-qt), PowerShell.

```powershell
git clone --recurse-submodules https://github.com/BKNadir/Nodelight
cd Nodelight
.\scripts\build-bridge.ps1      # tests + dist\Nodelight.exe
.\setup-deps.ps1
scripts\build-arch.bat release  # depuis une invite de commandes Qt + MSVC
.\scripts\package.ps1           # dist\Nodelight-win-x64.zip
```

## Ce qui est repris d'Artemis, et ce qui ne l'est pas

Artemis est un client **Android** pour Apollo. Beaucoup de ses fonctions n'ont pas de sens sur
Windows ou existent déjà dans moonlight-qt.

| Fonction Artemis | État ici |
| --- | --- |
| Écran virtuel Apollo | **Fait** : case dans les réglages hôte + `--virtual-display` |
| Résolution et débit personnalisés | Déjà présents dans moonlight-qt |
| Commandes serveur Apollo | Non fait : nécessite le fork de `moonlight-common-c` d'Artemis (`LiSendExecServerCmd`) |
| Synchronisation du presse-papiers | Non fait : appels HTTP `actions/clipboard` + intégration `QClipboard` à écrire |
| Boutons virtuels, trackpad tactile, DeX, portrait, SBS 3D… | Spécifiques à Android, non applicables |

## État de vérification — à lire

Vérifié dans l'environnement de développement (sans Windows) :
- tests du relais (TCP aller-retour avec demi-fermeture, flux UDP indépendants par client, expiration
  des flux inactifs, liaison « tout ou rien », arrêt propre) : OK avec `-race` ;
- tests de l'analyse de configuration, des profils et de la saisie au premier lancement ;
- tests du contrôleur (connexion avec ou sans PC, déconnexion, connexion en attente d'autorisation,
  erreurs, nœud de sortie à chaud, liste des PC, changement et suppression de profil) et de l'API locale
  (jeton, garde `Host`, validation), avec un faux Tailscale, exécutés plusieurs fois avec `-race` ;
- la logique JavaScript des pages QML a été exécutée sous Node avec les vrais JSON du code Go ;
- les modifications du client s'appliquent proprement sur le commit amont `3178022` de moonlight-qt.

Constaté par l'auteur sur Windows : le build GitHub Actions se termine, l'application se lance, les
icônes s'affichent aux bons endroits et la page Tailscale s'ouvre dans Moonlight.

**Non vérifié** (à tester) :
- la connexion réelle à Tailscale depuis l'application (autorisation, liste des nœuds de sortie) ;
- l'acheminement réel du trafic par un nœud de sortie avec `tsnet` (UDP en particulier) ;
- un flux de streaming complet à travers Tailscale, et le comportement du pare-feu/ACL ;
- l'attache au terminal, le journal et les boîtes de dialogue de `Nodelight.exe` dans tous les cas.

Si une étape échoue, ouvrez une issue avec le message d'erreur (et `Nodelight.log`) : les causes les
plus probables sont un décalage d'API `tsnet` (à adapter dans `bridge/cmd/nodelight/tunnel_ts.go`) ou
un port déjà utilisé sur l'adresse loopback (un Sunshine local, par exemple).

## Signature de code

Les exécutables ne sont **pas encore signés** (SmartScreen peut afficher un avertissement : « Informations complémentaires » → « Exécuter quand même »). Une demande de signature gratuite auprès de la [SignPath Foundation](https://signpath.org/) est en cours. Politique complète (en anglais) : [Code signing policy](README.md#code-signing-policy).

## Licence

GPL-3.0 (voir `LICENSE`) : le code est dérivé de moonlight-qt. Attributions
détaillées dans `licenses/NOTICE.md`.
