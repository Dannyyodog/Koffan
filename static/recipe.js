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
        editingStepId: null,
        editStepContent: '',

        // Inline add forms
        addingIngredient: false,
        newIngredientName: '',
        newIngredientQty: 1,
        newIngredientUnit: 'whole',
        addingStep: false,
        newStepContent: '',

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

        // WebSocket
        ws: null,
        _wsInitialized: false,
        _reconnectAttempts: 0,
        _pingInterval: null,
        _ingredientsSortable: null,
        _stepsSortable: null,

        // Unit list — keep in sync with handlers/recipes.go validUnits.
        unitOptions: ['whole', 'tsp', 'tbsp', 'cup', 'fl_oz', 'oz', 'lb', 'g', 'kg', 'ml', 'l', 'to_taste'],

        t(key, params) {
            return window.t ? window.t(key, params) : key;
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

        formatIngredient(ing) {
            const unit = this.t('units.' + ing.unit);
            if (ing.unit === 'to_taste' || ing.quantity == null) {
                return unit + ' ' + ing.name;
            }
            return ing.quantity + ' ' + unit + ' ' + ing.name;
        },

        // ===== Ingredients =====

        startEditIngredient(ing) {
            this.cancelAddIngredient();
            this.cancelEditStep();
            this.editingIngredientId = ing.id;
            this.editIngredientName = ing.name;
            this.editIngredientUnit = ing.unit;
            this.editIngredientQty = (ing.quantity != null ? ing.quantity : 1);
        },

        cancelEditIngredient() {
            this.editingIngredientId = null;
            this.editIngredientName = '';
        },

        async saveIngredient(ing) {
            const name = (this.editIngredientName || '').trim();
            if (!name) return;
            const fd = new FormData();
            fd.append('name', name);
            fd.append('unit', this.editIngredientUnit);
            if (this.editIngredientUnit !== 'to_taste') {
                fd.append('quantity', String(this.editIngredientQty || 1));
            }
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
            this.$nextTick(() => this.$refs.newIngName?.focus());
        },

        cancelAddIngredient() {
            this.addingIngredient = false;
            this.newIngredientName = '';
        },

        async submitAddIngredient() {
            const name = (this.newIngredientName || '').trim();
            if (!name) return;
            const fd = new FormData();
            fd.append('name', name);
            fd.append('unit', this.newIngredientUnit);
            if (this.newIngredientUnit !== 'to_taste') {
                fd.append('quantity', String(this.newIngredientQty || 1));
            }
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
                }
            } catch (e) {
                console.error('[Recipe] WS parse failed:', e);
            }
        },
    };
}
