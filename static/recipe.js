// recipe.js — Alpine component for the recipe detail page (templates/recipe.html).
// Loaded everywhere via the layout, but only attaches when an x-data="recipeView(N)"
// node is present. Talks to the existing handlers/recipes.go HTTP routes.

function recipeView(recipeId) {
    return {
        recipeId: recipeId,
        recipe: { id: recipeId, name: '', description: '', ingredients: [], steps: [] },

        // Per-row edit state
        editingIngredientId: null,
        editIngredientName: '',
        editIngredientQty: 1,
        editIngredientUnit: 'whole',
        editIngredientNotes: '',
        editingStepId: null,
        editStepContent: '',

        // Inline add forms
        addingIngredient: false,
        newIngredientName: '',
        newIngredientQty: 1,
        newIngredientUnit: 'whole',
        newIngredientNotes: '',
        addingStep: false,
        newStepContent: '',

        // Ingredient image lightbox + upload
        uploadingIngredientName: null,    // lower-cased name currently uploading, or null
        ingredientLightbox: { open: false, name: '', imagePath: '' },

        // Floating + popup
        showActionMenu: false,

        // Modals
        showRenameModal: false,
        renameName: '',
        renameDescription: '',

        showApplyModal: false,
        applyTarget: 'existing',          // 'existing' | 'new'
        applyListId: null,
        applyListName: '',
        applySelections: {},              // { ingredientId: true }
        availableLists: [],

        // Cover image upload
        uploadingCover: false,
        coverLightboxOpen: false,

        // WebSocket
        ws: null,
        _wsInitialized: false,
        _reconnectAttempts: 0,
        _pingInterval: null,
        _ingredientsSortable: null,
        _stepsSortable: null,

        // Unit lists — kept grouped to match the optgroup-rendered <select>.
        // Stay in sync with handlers/recipes.go validUnits + db.MeasurementUnits/PackageUnits.
        cookingUnits: ['tsp', 'tbsp', 'cup', 'fl_oz', 'oz', 'lb', 'g', 'kg', 'ml', 'l'],
        packageUnits: ['whole', 'can', 'jar', 'bottle', 'package', 'bunch', 'head', 'dozen', 'slice', 'loaf', 'clove'],
        // Flat list (cooking + packaging + to_taste) for any code path that
        // wants every valid unit in one array.
        get unitOptions() {
            return [...this.cookingUnits, ...this.packageUnits, 'to_taste'];
        },

        t(key, params) {
            return window.t ? window.t(key, params) : key;
        },

        // ===== Quantity formatting + steppers =====

        // formatQuantity converts a float quantity into a compact display string.
        // Maps 0.25/0.5/0.75 + 1/3 + 2/3 to unicode glyphs; supports mixed numbers
        // ("1½", "2¾"); falls back to up-to-2-decimal for anything else.
        formatQuantity(q) {
            if (q == null || isNaN(q)) return '';
            if (q === 0) return '0';
            const whole = Math.trunc(q);
            const frac = +(q - whole).toFixed(2);
            const fracMap = {
                0:    '',
                0.25: '¼',
                0.5:  '½',
                0.75: '¾',
                0.33: '⅓',
                0.34: '⅓',
                0.67: '⅔',
                0.66: '⅔',
            };
            if (fracMap[frac] !== undefined) {
                if (whole === 0) return fracMap[frac] || '0';
                return fracMap[frac] === '' ? String(whole) : (whole + fracMap[frac]);
            }
            // Anything else: trim trailing zeros from up-to-2 decimals.
            return parseFloat(q.toFixed(2)).toString();
        },

        incrementQuantity(current) {
            const v = (typeof current === 'number' ? current : parseFloat(current)) || 0;
            return Math.round((v + 0.25) * 100) / 100;
        },

        decrementQuantity(current) {
            const v = (typeof current === 'number' ? current : parseFloat(current)) || 0;
            const next = Math.round((v - 0.25) * 100) / 100;
            return next < 0.25 ? 0.25 : next;
        },

        async init() {
            await this.loadRecipe();
            this.initWebSocket();
            // Defer sortable init until DOM has the rows.
            this.$nextTick(() => this.initSortable());
        },

        async loadRecipe() {
            try {
                const response = await fetch('/recipes/' + this.recipeId, {
                    headers: { 'Accept': 'application/json' }
                });
                if (!response.ok) {
                    if (response.status === 404) {
                        window.location.href = '/';
                    }
                    return;
                }
                const data = await response.json();
                if (!data.ingredients) data.ingredients = [];
                if (!data.steps) data.steps = [];
                this.recipe = data;
                // Re-attach Sortable to fresh DOM nodes after the templates re-render.
                this.$nextTick(() => this.initSortable());
            } catch (e) {
                console.error('[Recipe] loadRecipe failed:', e);
            }
        },

        // formatIngredient renders the visible row text. Uses unicode fractions
        // for nice quantities, falls back to decimals otherwise. Notes are
        // rendered separately by the template under the main row.
        formatIngredient(ing) {
            const unit = this.t('units.' + ing.unit);
            if (ing.unit === 'to_taste' || ing.quantity == null) {
                return unit + ' ' + ing.name;
            }
            return this.formatQuantity(ing.quantity) + ' ' + unit + ' ' + ing.name;
        },

        // ===== Ingredients =====

        startEditIngredient(ing) {
            this.cancelAddIngredient();
            this.cancelEditStep();
            this.editingIngredientId = ing.id;
            this.editIngredientName = ing.name;
            this.editIngredientUnit = ing.unit;
            this.editIngredientQty = (ing.quantity != null ? ing.quantity : 1);
            this.editIngredientNotes = ing.notes || '';
        },

        cancelEditIngredient() {
            this.editingIngredientId = null;
            this.editIngredientName = '';
            this.editIngredientNotes = '';
        },

        async saveIngredient(ing) {
            const name = (this.editIngredientName || '').trim();
            if (!name) return;
            const fd = new FormData();
            fd.append('name', name);
            fd.append('unit', this.editIngredientUnit);
            if (this.editIngredientUnit !== 'to_taste') {
                fd.append('quantity', String(this.editIngredientQty || 0.25));
            }
            fd.append('notes', this.editIngredientNotes || '');
            try {
                const response = await fetch('/recipes/' + this.recipeId + '/ingredients/' + ing.id, {
                    method: 'PUT',
                    body: fd
                });
                if (!response.ok) {
                    const err = await response.text();
                    alert(err || this.t('error.update_failed'));
                    return;
                }
                this.editingIngredientId = null;
                await this.loadRecipe();
            } catch (e) {
                console.error('[Recipe] saveIngredient failed:', e);
                alert(this.t('error.update_failed'));
            }
        },

        async deleteIngredient(ing) {
            if (!confirm(this.t('recipes.confirm_delete_ingredient'))) return;
            try {
                const response = await fetch('/recipes/' + this.recipeId + '/ingredients/' + ing.id, {
                    method: 'DELETE'
                });
                if (!response.ok) {
                    alert(this.t('error.delete_failed'));
                    return;
                }
                await this.loadRecipe();
            } catch (e) {
                console.error('[Recipe] deleteIngredient failed:', e);
            }
        },

        startAddIngredient() {
            this.cancelEditIngredient();
            this.cancelAddStep();
            this.addingIngredient = true;
            this.newIngredientName = '';
            this.newIngredientQty = 1;
            this.newIngredientUnit = 'whole';
            this.newIngredientNotes = '';
            this.$nextTick(() => this.$refs.newIngName?.focus());
        },

        cancelAddIngredient() {
            this.addingIngredient = false;
            this.newIngredientName = '';
            this.newIngredientNotes = '';
        },

        async submitAddIngredient() {
            const name = (this.newIngredientName || '').trim();
            if (!name) return;
            const fd = new FormData();
            fd.append('name', name);
            fd.append('unit', this.newIngredientUnit);
            if (this.newIngredientUnit !== 'to_taste') {
                fd.append('quantity', String(this.newIngredientQty || 0.25));
            }
            fd.append('notes', this.newIngredientNotes || '');
            try {
                const response = await fetch('/recipes/' + this.recipeId + '/ingredients', {
                    method: 'POST',
                    body: fd
                });
                if (!response.ok) {
                    const err = await response.text();
                    alert(err || this.t('error.create_failed'));
                    return;
                }
                this.addingIngredient = false;
                this.newIngredientName = '';
                this.newIngredientQty = 1;
                this.newIngredientUnit = 'whole';
                this.newIngredientNotes = '';
                await this.loadRecipe();
            } catch (e) {
                console.error('[Recipe] submitAddIngredient failed:', e);
                alert(this.t('error.create_failed'));
            }
        },

        // ===== Steps =====

        startEditStep(step) {
            this.cancelAddStep();
            this.cancelEditIngredient();
            this.editingStepId = step.id;
            this.editStepContent = step.content;
        },

        cancelEditStep() {
            this.editingStepId = null;
            this.editStepContent = '';
        },

        async saveStep(step) {
            const content = (this.editStepContent || '').trim();
            if (!content) return;
            const fd = new FormData();
            fd.append('content', content);
            try {
                const response = await fetch('/recipes/' + this.recipeId + '/steps/' + step.id, {
                    method: 'PUT',
                    body: fd
                });
                if (!response.ok) {
                    alert(this.t('error.update_failed'));
                    return;
                }
                this.editingStepId = null;
                await this.loadRecipe();
            } catch (e) {
                console.error('[Recipe] saveStep failed:', e);
                alert(this.t('error.update_failed'));
            }
        },

        async deleteStep(step) {
            if (!confirm(this.t('recipes.confirm_delete_step'))) return;
            try {
                const response = await fetch('/recipes/' + this.recipeId + '/steps/' + step.id, {
                    method: 'DELETE'
                });
                if (!response.ok) {
                    alert(this.t('error.delete_failed'));
                    return;
                }
                await this.loadRecipe();
            } catch (e) {
                console.error('[Recipe] deleteStep failed:', e);
            }
        },

        startAddStep() {
            this.cancelEditStep();
            this.cancelAddIngredient();
            this.addingStep = true;
            this.newStepContent = '';
            this.$nextTick(() => this.$refs.newStepContent?.focus());
        },

        cancelAddStep() {
            this.addingStep = false;
            this.newStepContent = '';
        },

        async submitAddStep() {
            const content = (this.newStepContent || '').trim();
            if (!content) return;
            const fd = new FormData();
            fd.append('content', content);
            try {
                const response = await fetch('/recipes/' + this.recipeId + '/steps', {
                    method: 'POST',
                    body: fd
                });
                if (!response.ok) {
                    alert(this.t('error.create_failed'));
                    return;
                }
                this.addingStep = false;
                this.newStepContent = '';
                await this.loadRecipe();
            } catch (e) {
                console.error('[Recipe] submitAddStep failed:', e);
                alert(this.t('error.create_failed'));
            }
        },

        // ===== Rename + delete recipe =====

        openRenameModal() {
            this.renameName = this.recipe.name;
            this.renameDescription = this.recipe.description || '';
            this.showRenameModal = true;
        },

        async saveRename() {
            const name = (this.renameName || '').trim();
            if (!name) return;
            const fd = new FormData();
            fd.append('name', name);
            fd.append('description', this.renameDescription || '');
            try {
                const response = await fetch('/recipes/' + this.recipeId, {
                    method: 'PUT',
                    body: fd
                });
                if (!response.ok) {
                    alert(this.t('error.update_failed'));
                    return;
                }
                this.showRenameModal = false;
                await this.loadRecipe();
            } catch (e) {
                console.error('[Recipe] saveRename failed:', e);
                alert(this.t('error.update_failed'));
            }
        },

        async confirmDeleteRecipe() {
            if (!confirm(this.t('recipes.confirm_delete', { name: this.recipe.name }))) return;
            try {
                const response = await fetch('/recipes/' + this.recipeId, { method: 'DELETE' });
                if (!response.ok) {
                    alert(this.t('error.delete_failed'));
                    return;
                }
                window.location.href = '/';
            } catch (e) {
                console.error('[Recipe] confirmDeleteRecipe failed:', e);
            }
        },

        // ===== Apply-to-list modal =====

        async openApplyModal() {
            // Default selection: every ingredient checked.
            this.applySelections = {};
            (this.recipe.ingredients || []).forEach(i => { this.applySelections[i.id] = true; });

            this.applyListName = this.recipe.name || '';
            this.applyTarget = 'existing';
            this.applyListId = null;

            // Fetch lists fresh every open so newly-created lists appear.
            try {
                const response = await fetch('/lists?format=json', { headers: { 'Accept': 'application/json' } });
                if (response.ok) {
                    const lists = await response.json();
                    this.availableLists = Array.isArray(lists) ? lists : [];
                    if (this.availableLists.length === 0) {
                        this.applyTarget = 'new';
                    } else {
                        this.applyListId = this.availableLists[0].id;
                    }
                }
            } catch (e) {
                console.error('[Recipe] openApplyModal lists fetch failed:', e);
                this.availableLists = [];
                this.applyTarget = 'new';
            }
            this.showApplyModal = true;
        },

        applySelectedCount() {
            return Object.values(this.applySelections).filter(v => v).length;
        },

        async submitApply() {
            const selected = (this.recipe.ingredients || [])
                .filter(i => this.applySelections[i.id])
                .map(i => i.id);
            if (selected.length === 0) return;

            const fd = new FormData();
            fd.append('target', this.applyTarget);
            if (this.applyTarget === 'existing') {
                if (!this.applyListId) return;
                fd.append('list_id', String(this.applyListId));
            } else {
                const name = (this.applyListName || '').trim();
                if (!name) return;
                fd.append('list_name', name);
            }
            selected.forEach(id => fd.append('ingredient_ids[]', String(id)));

            try {
                const response = await fetch('/recipes/' + this.recipeId + '/apply', {
                    method: 'POST',
                    headers: { 'HX-Request': 'true' },
                    body: fd
                });
                if (!response.ok) {
                    const err = await response.text();
                    alert(err || this.t('error.apply_failed'));
                    return;
                }
                // Backend returns HX-Redirect: /lists/<id>. Honor it.
                const redirect = response.headers.get('HX-Redirect');
                if (redirect) {
                    window.location.href = redirect;
                } else {
                    this.showApplyModal = false;
                }
            } catch (e) {
                console.error('[Recipe] submitApply failed:', e);
                alert(this.t('error.apply_failed'));
            }
        },

        // ===== Cover image =====

        // Trigger the OS file picker via the hidden input. iOS Safari shows
        // its standard sheet (Photo Library / Take Photo / Choose File) for
        // accept="image/*".
        triggerCoverUpload() {
            const input = document.getElementById('cover-image-input');
            if (input) input.click();
        },

        // POST the chosen file to /recipes/:id/cover-image. Server handles
        // size/format validation, HEIC->JPEG re-encoding, sha256 dedup, and
        // deleting the previous cover from disk. Response carries the new
        // URL; we update local state and the WS broadcast tells other tabs.
        async uploadCover(file) {
            if (!file || !this.recipe?.id) return;
            this.uploadingCover = true;
            const formData = new FormData();
            formData.append('image', file);
            try {
                const response = await fetch('/recipes/' + this.recipe.id + '/cover-image', {
                    method: 'POST',
                    body: formData,
                });
                if (!response.ok) {
                    const errText = await response.text();
                    alert(errText || this.t('error.image_save_failed'));
                    return;
                }
                const data = await response.json();
                if (data && data.cover_image_url) {
                    this.recipe.cover_image_path = data.cover_image_url.replace(/^\/uploads\//, '');
                }
            } catch (e) {
                console.error('[Recipe] uploadCover failed:', e);
                alert(this.t('error.image_save_failed'));
            } finally {
                this.uploadingCover = false;
                // Clear the input so re-uploading the same file fires @change again.
                const input = document.getElementById('cover-image-input');
                if (input) input.value = '';
            }
        },

        openCoverLightbox() {
            if (!this.recipe?.cover_image_path) return;
            this.coverLightboxOpen = true;
        },

        closeCoverLightbox() {
            this.coverLightboxOpen = false;
        },

        async confirmRemoveCover() {
            if (!this.recipe?.id) return;
            if (!confirm(this.t('recipes.confirm_remove_cover'))) return;
            try {
                const response = await fetch('/recipes/' + this.recipe.id + '/cover-image', { method: 'DELETE' });
                if (!response.ok) {
                    const errText = await response.text();
                    alert(errText || this.t('error.image_save_failed'));
                    return;
                }
                this.recipe.cover_image_path = null;
                this.closeCoverLightbox();
            } catch (e) {
                console.error('[Recipe] confirmRemoveCover failed:', e);
                alert(this.t('error.image_save_failed'));
            }
        },

        // ===== Step completion =====

        // Toggle a step's completed flag. Optimistic UI: flip the local bool
        // first so the checkbox reacts immediately, then confirm via the server.
        // The server's broadcast (recipe_step_completed_changed) will land and
        // re-set the flag — by then it should already match, so this is a no-op.
        async toggleStepCompleted(stepId) {
            const idx = (this.recipe.steps || []).findIndex(s => s.id === stepId);
            if (idx < 0) return;
            const wasCompleted = !!this.recipe.steps[idx].completed;
            this.recipe.steps[idx].completed = !wasCompleted;
            try {
                const response = await fetch(
                    '/recipes/' + this.recipeId + '/steps/' + stepId + '/toggle',
                    { method: 'POST' }
                );
                if (!response.ok) {
                    // Revert on failure.
                    this.recipe.steps[idx].completed = wasCompleted;
                    alert(this.t('error.toggle_failed'));
                }
            } catch (e) {
                console.error('[Recipe] toggleStepCompleted failed:', e);
                this.recipe.steps[idx].completed = wasCompleted;
            }
        },

        async confirmResetSteps() {
            if (!confirm(this.t('recipes.confirm_reset_steps'))) return;
            try {
                const response = await fetch(
                    '/recipes/' + this.recipeId + '/steps/reset-completed',
                    { method: 'POST' }
                );
                if (!response.ok) {
                    alert(this.t('error.update_failed'));
                    return;
                }
                // Optimistic: clear all locally so the button + strikethrough
                // disappear before the WS broadcast arrives.
                (this.recipe.steps || []).forEach(s => { s.completed = false; });
            } catch (e) {
                console.error('[Recipe] confirmResetSteps failed:', e);
                alert(this.t('error.update_failed'));
            }
        },

        // ===== Ingredient images (image-by-name endpoint) =====

        // Open the lightbox for a specific ingredient. If the ingredient has
        // no image, the lightbox renders an "Add ingredient image" button that
        // delegates to triggerIngredientImageUpload.
        openIngredientLightbox(ingredient) {
            this.ingredientLightbox = {
                open: true,
                name: ingredient.name,
                imagePath: ingredient.image_path || '',
            };
        },

        closeIngredientLightbox() {
            this.ingredientLightbox = { open: false, name: '', imagePath: '' };
        },

        // Trigger the OS file picker for an ingredient. The change handler
        // calls uploadIngredientImage.
        triggerIngredientImageUpload(name) {
            // Stash the target name on the input so the @change handler knows
            // who it belongs to (the lightbox closes between click + change).
            const input = document.getElementById('ingredient-image-input');
            if (!input) return;
            input.dataset.targetName = name;
            input.click();
        },

        async uploadIngredientImage(name, file) {
            if (!file || !name) return;
            const target = name;
            this.uploadingIngredientName = target.toLowerCase();
            const formData = new FormData();
            formData.append('image', file);
            try {
                const response = await fetch(
                    '/image-by-name/' + encodeURIComponent(target),
                    { method: 'POST', body: formData }
                );
                if (!response.ok) {
                    const err = await response.text();
                    alert(err || this.t('error.image_save_failed'));
                    return;
                }
                const data = await response.json();
                const newPath = data && data.image_url
                    ? data.image_url.replace(/^\/uploads\//, '')
                    : '';
                // Update every matching ingredient in this recipe so the
                // thumbnails refresh without a full reload. The WS broadcast
                // will also arrive but this keeps the UI snappy.
                this.applyIngredientImageByName(target, newPath);
                if (this.ingredientLightbox.open && this.ingredientLightbox.name &&
                    this.ingredientLightbox.name.toLowerCase() === target.toLowerCase()) {
                    this.ingredientLightbox.imagePath = newPath;
                }
            } catch (e) {
                console.error('[Recipe] uploadIngredientImage failed:', e);
                alert(this.t('error.image_save_failed'));
            } finally {
                this.uploadingIngredientName = null;
                const input = document.getElementById('ingredient-image-input');
                if (input) {
                    input.value = '';
                    delete input.dataset.targetName;
                }
            }
        },

        async confirmRemoveIngredientImage(name) {
            if (!name) return;
            if (!confirm(this.t('recipes.confirm_remove_ingredient_image'))) return;
            try {
                const response = await fetch(
                    '/image-by-name/' + encodeURIComponent(name),
                    { method: 'DELETE' }
                );
                if (!response.ok) {
                    const err = await response.text();
                    alert(err || this.t('error.image_save_failed'));
                    return;
                }
                this.applyIngredientImageByName(name, '');
                this.closeIngredientLightbox();
            } catch (e) {
                console.error('[Recipe] confirmRemoveIngredientImage failed:', e);
                alert(this.t('error.image_save_failed'));
            }
        },

        // Walk this recipe's ingredients and update image_path for every name
        // that case-insensitively matches `name`. Used by both the local upload
        // path and the WebSocket dispatcher (item_image_updated event).
        applyIngredientImageByName(name, newPath) {
            const target = (name || '').toLowerCase();
            if (!this.recipe.ingredients) return;
            this.recipe.ingredients.forEach(ing => {
                if ((ing.name || '').toLowerCase() === target) {
                    ing.image_path = newPath || '';
                }
            });
        },

        // ===== SortableJS for drag-reorder =====

        initSortable() {
            if (typeof Sortable === 'undefined') return;

            const ingList = document.getElementById('ingredients-list');
            if (ingList) {
                if (this._ingredientsSortable) this._ingredientsSortable.destroy();
                this._ingredientsSortable = new Sortable(ingList, {
                    handle: '.drag-handle',
                    draggable: '.ingredient-row',
                    animation: 200,
                    ghostClass: 'sortable-ghost',
                    chosenClass: 'sortable-chosen',
                    delay: 150,
                    delayOnTouchOnly: true,
                    forceFallback: true,
                    fallbackOnBody: true,
                    onEnd: () => this.persistIngredientOrder(),
                });
            }

            const stepList = document.getElementById('steps-list');
            if (stepList) {
                if (this._stepsSortable) this._stepsSortable.destroy();
                this._stepsSortable = new Sortable(stepList, {
                    handle: '.drag-handle',
                    draggable: '.step-row',
                    animation: 200,
                    ghostClass: 'sortable-ghost',
                    chosenClass: 'sortable-chosen',
                    delay: 150,
                    delayOnTouchOnly: true,
                    forceFallback: true,
                    fallbackOnBody: true,
                    onEnd: () => this.persistStepOrder(),
                });
            }
        },

        async persistIngredientOrder() {
            const list = document.getElementById('ingredients-list');
            if (!list) return;
            const ids = Array.from(list.querySelectorAll('[data-ingredient-id]'))
                .map(el => el.dataset.ingredientId)
                .filter(Boolean);
            if (ids.length === 0) return;

            const fd = new FormData();
            ids.forEach(id => fd.append('ordered_ids[]', id));
            try {
                const response = await fetch('/recipes/' + this.recipeId + '/ingredients/reorder', {
                    method: 'POST',
                    body: fd
                });
                if (!response.ok) {
                    console.error('[Recipe] reorder ingredients failed');
                }
                await this.loadRecipe();
            } catch (e) {
                console.error('[Recipe] persistIngredientOrder failed:', e);
            }
        },

        async persistStepOrder() {
            const list = document.getElementById('steps-list');
            if (!list) return;
            const ids = Array.from(list.querySelectorAll('[data-step-id]'))
                .map(el => el.dataset.stepId)
                .filter(Boolean);
            if (ids.length === 0) return;

            const fd = new FormData();
            ids.forEach(id => fd.append('ordered_ids[]', id));
            try {
                const response = await fetch('/recipes/' + this.recipeId + '/steps/reorder', {
                    method: 'POST',
                    body: fd
                });
                if (!response.ok) {
                    console.error('[Recipe] reorder steps failed');
                }
                await this.loadRecipe();
            } catch (e) {
                console.error('[Recipe] persistStepOrder failed:', e);
            }
        },

        // ===== WebSocket sync =====

        initWebSocket() {
            if (this._wsInitialized) return;
            this._wsInitialized = true;
            this.connectWS();
        },

        connectWS() {
            if (this.ws) {
                try { this.ws.close(); } catch (_) {}
                this.ws = null;
            }
            if (this._pingInterval) {
                clearInterval(this._pingInterval);
                this._pingInterval = null;
            }
            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            const wsUrl = protocol + '//' + window.location.host + '/ws';
            try {
                this.ws = new WebSocket(wsUrl);
                this.ws.onopen = () => {
                    this._reconnectAttempts = 0;
                    this._pingInterval = setInterval(() => {
                        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                            this.ws.send(JSON.stringify({ type: 'ping' }));
                        }
                    }, 30000);
                };
                this.ws.onclose = () => {
                    if (this._pingInterval) {
                        clearInterval(this._pingInterval);
                        this._pingInterval = null;
                    }
                    this.scheduleWSReconnect();
                };
                this.ws.onmessage = (e) => this.handleWSMessage(e.data);
                this.ws.onerror = (e) => console.error('[Recipe] WS error:', e);
            } catch (e) {
                this.scheduleWSReconnect();
            }
        },

        scheduleWSReconnect() {
            if (this._reconnectAttempts >= 5) return;
            this._reconnectAttempts++;
            const delay = Math.min(1000 * Math.pow(2, this._reconnectAttempts), 30000);
            setTimeout(() => this.connectWS(), delay);
        },

        handleWSMessage(data) {
            try {
                const message = JSON.parse(data);
                switch (message.type) {
                    case 'recipe_updated':
                        if (message.data?.id === this.recipeId) this.loadRecipe();
                        break;
                    case 'recipe_deleted':
                        if (message.data?.id === this.recipeId) {
                            window.location.href = '/';
                        }
                        break;
                    case 'recipe_ingredient_created':
                    case 'recipe_ingredient_updated':
                    case 'recipe_ingredient_deleted':
                    case 'recipe_ingredients_reordered':
                        if (message.data?.recipe_id === this.recipeId ||
                            (message.data && this.recipe.ingredients.some(i => i.id === message.data.id))) {
                            this.loadRecipe();
                        } else {
                            // Be lenient: if the payload doesn't carry recipe_id, refresh anyway —
                            // ingredient-deleted only includes {id}, and we don't track ownership.
                            this.loadRecipe();
                        }
                        break;
                    case 'recipe_step_created':
                    case 'recipe_step_updated':
                    case 'recipe_step_deleted':
                    case 'recipe_steps_reordered':
                        this.loadRecipe();
                        break;
                    case 'recipe_step_completed_changed':
                        // Payload: { recipe_id, step_id, completed }
                        if (message.data?.recipe_id === this.recipe?.id) {
                            const sid = message.data?.step_id;
                            const idx = (this.recipe.steps || []).findIndex(s => s.id === sid);
                            if (idx >= 0) {
                                this.recipe.steps[idx].completed = !!message.data?.completed;
                            }
                        }
                        break;
                    case 'recipe_steps_reset':
                        if (message.data?.recipe_id === this.recipe?.id) {
                            (this.recipe.steps || []).forEach(s => { s.completed = false; });
                        }
                        break;
                    case 'recipe_cover_updated':
                        // Payload: { recipe_id, cover_image_url }; empty url means cleared.
                        if (message.data?.recipe_id === this.recipe?.id) {
                            const url = message.data?.cover_image_url || '';
                            this.recipe.cover_image_path = url ? url.replace(/^\/uploads\//, '') : null;
                        }
                        break;
                    case 'item_image_updated':
                        // Shared event: same broadcast item-image upload uses.
                        // Walk this recipe's ingredients and update any whose name
                        // matches case-insensitively. Lightbox state syncs too.
                        if (message.data?.name) {
                            const url = message.data?.image_url || '';
                            const newPath = url ? url.replace(/^\/uploads\//, '') : '';
                            this.applyIngredientImageByName(message.data.name, newPath);
                            if (this.ingredientLightbox.open && this.ingredientLightbox.name &&
                                this.ingredientLightbox.name.toLowerCase() === message.data.name.toLowerCase()) {
                                this.ingredientLightbox.imagePath = newPath;
                            }
                        }
                        break;
                }
            } catch (e) {
                console.error('[Recipe] WS parse failed:', e);
            }
        },
    };
}
