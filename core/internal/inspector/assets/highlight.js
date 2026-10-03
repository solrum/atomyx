import { on } from './bus.js';
import { parseBounds } from './geometry.js';

const PERCENT = 100;

const REASONS = {
  stale: 'The selected node comes from an earlier capture, so no highlight is drawn.',
  unpaired: 'The tree and the screenshot were captured at different moments, so no highlight is drawn.',
  unmatched: 'This screenshot cannot be matched to the tree, so no highlight is drawn.',
  noBounds: 'The selected node reports no bounds, so no highlight is drawn.',
};

// Draws the selected node's bounds over the screenshot and turns a click
// on the screenshot into a selection. The server decides whether the
// tree and the screenshot belong together and what scale takes device
// units to screenshot pixels, so this module only multiplies by that
// scale. Position is expressed as a share of the image's own pixel size,
// which keeps the box aligned however the image is scaled on screen.
export function createHighlight({ img, overlay, noteEl, onPick }) {
  let shot = null;
  let treeRevision = null;
  let selection = null;

  const canMatch = () => shot !== null && shot.paired && shot.highlightable && shot.scale > 0;

  function hide(reason) {
    overlay.hidden = true;
    noteEl.textContent = reason || '';
    noteEl.hidden = !reason;
  }

  function reasonToHide() {
    if (selection === null || shot === null) {
      return '';
    }
    if (selection.revision !== treeRevision) {
      return REASONS.stale;
    }
    if (!shot.paired) {
      return REASONS.unpaired;
    }
    return shot.highlightable ? '' : REASONS.unmatched;
  }

  function update() {
    img.classList.toggle('pickable', canMatch());
    const reason = reasonToHide();
    const bounds = selection && parseBounds(selection.node.attributes && selection.node.attributes.bounds);
    if (reason || selection === null || shot === null) {
      hide(reason);
      return;
    }
    if (!bounds) {
      hide(REASONS.noBounds);
      return;
    }
    const width = img.naturalWidth;
    const height = img.naturalHeight;
    const { scale } = shot;
    overlay.style.left = `${(bounds.left * scale / width) * PERCENT}%`;
    overlay.style.top = `${(bounds.top * scale / height) * PERCENT}%`;
    overlay.style.width = `${((bounds.right - bounds.left) * scale / width) * PERCENT}%`;
    overlay.style.height = `${((bounds.bottom - bounds.top) * scale / height) * PERCENT}%`;
    hide('');
    overlay.hidden = false;
  }

  img.addEventListener('click', (event) => {
    if (!canMatch()) {
      return;
    }
    const rect = img.getBoundingClientRect();
    const x = ((event.clientX - rect.left) / rect.width) * img.naturalWidth / shot.scale;
    const y = ((event.clientY - rect.top) / rect.height) * img.naturalHeight / shot.scale;
    if (!onPick(x, y)) {
      noteEl.textContent = 'No node contains that point.';
      noteEl.hidden = false;
    }
  });

  on('screenshot', (facts) => {
    shot = facts;
    update();
  });
  on('tree', (response) => {
    treeRevision = response.revision;
    update();
  });
  on('selection', (next) => {
    selection = next;
    update();
  });
}
