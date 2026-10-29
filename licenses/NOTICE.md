# Licences et attributions

- **moonlight-qt** (https://github.com/moonlight-stream/moonlight-qt) est sous GPL-3.0.
  Les fichiers de `patches/moonlight-qt/` en sont des travaux dérivés et sont donc
  distribués sous GPL-3.0. Un binaire Moonlight modifié doit être accompagné de ses
  sources : ce dépôt (patchs + `scripts/pins.ps1`) permet de les reconstituer.
- **Tailscale `tsnet`** (https://github.com/tailscale/tailscale) est sous BSD-3-Clause.
  Nodelight l'utilise comme bibliothèque.
- **Artemis** (https://github.com/ClassicOldSong/moonlight-android) est sous GPL-3.0.
  Aucun code d'Artemis n'est copié ici : l'option d'écran virtuel est une
  réimplémentation du paramètre de lancement `virtualDisplay` observé dans son client.
- **PortableTailscale** (https://github.com/CodeXTF2/PortableTailscale) n'a pas de
  licence publiée : aucun de ses fichiers n'est repris. Nodelight reprend seulement
  l'idée (nœud tsnet en espace utilisateur, identité stockée à côté de l'exécutable).

Tailscale est une marque de Tailscale Inc. Ce projet n'est affilié ni à Tailscale Inc.,
ni à Moonlight, ni à Artemis/Apollo.
