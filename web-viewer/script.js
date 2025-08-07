class RankingViewer {
    constructor() {
        this.allData = [];
        this.filteredData = [];
        this.currentPage = 1;
        this.pageSize = 25;
        this.sortColumn = 'datetime';
        this.sortDirection = 'desc';
        this.currentRegion = '1';
        this.regions = {};
        
        this.initializeEventListeners();
        this.loadRegionNames();
    }

    initializeEventListeners() {
        document.getElementById('loadData').addEventListener('click', () => this.loadData());
        document.getElementById('searchInput').addEventListener('input', () => this.applyFilters());
        document.getElementById('rankFilter').addEventListener('change', () => this.applyFilters());
        document.getElementById('minPoints').addEventListener('input', () => this.applyFilters());
        document.getElementById('maxPoints').addEventListener('input', () => this.applyFilters());
        document.getElementById('clearFilters').addEventListener('click', () => this.clearFilters());
        document.getElementById('exportData').addEventListener('click', () => this.exportData());
        document.getElementById('pageSize').addEventListener('change', (e) => {
            this.pageSize = e.target.value === 'all' ? this.filteredData.length : parseInt(e.target.value);
            this.currentPage = 1;
            this.renderTable();
        });
        document.getElementById('regionSelect').addEventListener('change', (e) => {
            this.currentRegion = e.target.value;
        });

        document.querySelectorAll('th[data-sort]').forEach(th => {
            th.addEventListener('click', () => this.sort(th.dataset.sort));
        });
    }

    async loadRegionNames() {
        try {
            const response = await fetch('/api/regions');
            if (response.ok) {
                this.regions = await response.json();
                this.updateRegionSelect();
            }
        } catch (error) {
            console.error('Failed to load region names:', error);
            // デフォルト値を使用
            this.regions = {
                '1': 'リージョン 1',
                '2': 'リージョン 2', 
                '3': 'リージョン 3',
                '4': 'リージョン 4',
                '5': 'リージョン 5',
                '6': 'リージョン 6'
            };
            this.updateRegionSelect();
        }
    }

    updateRegionSelect() {
        const regionSelect = document.getElementById('regionSelect');
        regionSelect.innerHTML = '';
        
        for (const [regionId, regionName] of Object.entries(this.regions)) {
            const option = document.createElement('option');
            option.value = regionId;
            option.textContent = regionName;
            regionSelect.appendChild(option);
        }
        
        // デフォルトで最初のリージョンを選択
        if (Object.keys(this.regions).length > 0) {
            regionSelect.value = Object.keys(this.regions)[0];
            this.currentRegion = Object.keys(this.regions)[0];
        }
    }

    async loadData() {
        this.showLoading(true);
        try {
            const region = this.currentRegion;
            await this.loadCSVData(region);
            
            this.applyFilters();
            this.updateStats();
            this.showLoading(false);
        } catch (error) {
            console.error('データ読込エラー:', error);
            alert('データの読み込みに失敗しました: ' + error.message);
            this.showLoading(false);
        }
    }


    async loadCSVData(region) {
        const response = await fetch(`/res/${region}/csv/datas.csv`);
        if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
        
        const csvText = await response.text();
        const lines = csvText.split('\n').filter(line => line.trim());
        const headers = lines[0].split(',');
        
        this.allData = [];
        
        for (let i = 1; i < lines.length; i++) {
            const values = this.parseCSVLine(lines[i]);
            if (values.length < 4) continue; // At least datetime, rank, name, points
            
            const entry = {
                datetime: values[0],
                timestamp: this.convertDateTimeToTimestamp(values[0]),
                rank: parseInt(values[1]) || 0,
                name: values[2] || 'Unknown',
                points: this.parsePoints(values[3]),
                pointsDisplay: values[3],
                // All time differences from CSV (1h to 180h)
                diff1h: values[4] || '-',
                diff3h: values[5] || '-',
                diff6h: values[6] || '-',
                diff9h: values[7] || '-',
                diff12h: values[8] || '-',
                diff15h: values[9] || '-',
                diff18h: values[10] || '-',
                diff21h: values[11] || '-',
                diff24h: values[12] || '-',
                diff36h: values[13] || '-',
                diff48h: values[14] || '-',
                diff60h: values[15] || '-',
                diff72h: values[16] || '-',
                diff84h: values[17] || '-',
                diff96h: values[18] || '-',
                diff108h: values[19] || '-',
                diff120h: values[20] || '-',
                diff132h: values[21] || '-',
                diff144h: values[22] || '-',
                diff156h: values[23] || '-',
                diff168h: values[24] || '-',
                diff180h: values[25] || '-'
            };
            
            this.allData.push(entry);
        }
    }

    parseCSVLine(line) {
        const result = [];
        let current = '';
        let inQuotes = false;
        
        for (let i = 0; i < line.length; i++) {
            const char = line[i];
            
            if (char === '"') {
                inQuotes = !inQuotes;
            } else if (char === ',' && !inQuotes) {
                result.push(current);
                current = '';
            } else {
                current += char;
            }
        }
        
        result.push(current);
        return result;
    }

    convertDateTimeToTimestamp(datetime) {
        const parts = datetime.match(/(\d{4})(\d{2})(\d{2})(\d{2})/);
        if (parts) {
            return parts[1] + parts[2] + parts[3] + parts[4];
        }
        return datetime;
    }

    parsePoints(pointStr) {
        if (!pointStr || pointStr === '-' || pointStr === '0') return 0;
        return parseInt(pointStr.replace(/,/g, '')) || 0;
    }


    applyFilters() {
        const searchTerm = document.getElementById('searchInput').value.toLowerCase();
        const rankFilter = document.getElementById('rankFilter').value;
        const minPoints = parseInt(document.getElementById('minPoints').value) || 0;
        const maxPoints = parseInt(document.getElementById('maxPoints').value) || Infinity;
        
        this.filteredData = this.allData.filter(entry => {
            if (searchTerm && !entry.name.toLowerCase().includes(searchTerm)) return false;
            
            if (rankFilter) {
                const [min, max] = rankFilter.split('-').map(Number);
                if (entry.rank < min || entry.rank > max) return false;
            }
            
            if (entry.points < minPoints || entry.points > maxPoints) return false;
            
            return true;
        });
        
        this.currentPage = 1;
        this.renderTable();
        this.updateStats();
    }

    clearFilters() {
        document.getElementById('searchInput').value = '';
        document.getElementById('rankFilter').value = '';
        document.getElementById('minPoints').value = '';
        document.getElementById('maxPoints').value = '';
        
        this.applyFilters();
    }

    sort(column) {
        if (this.sortColumn === column) {
            this.sortDirection = this.sortDirection === 'asc' ? 'desc' : 'asc';
        } else {
            this.sortColumn = column;
            this.sortDirection = 'asc';
        }
        
        this.filteredData.sort((a, b) => {
            let valA = a[column];
            let valB = b[column];
            
            if (column === 'points' || column === 'rank') {
                valA = typeof valA === 'number' ? valA : 0;
                valB = typeof valB === 'number' ? valB : 0;
            } else if (column === 'datetime') {
                valA = new Date(valA).getTime();
                valB = new Date(valB).getTime();
            }
            
            if (valA < valB) return this.sortDirection === 'asc' ? -1 : 1;
            if (valA > valB) return this.sortDirection === 'asc' ? 1 : -1;
            return 0;
        });
        
        this.renderTable();
        this.updateSortIndicators();
    }

    updateSortIndicators() {
        document.querySelectorAll('th[data-sort]').forEach(th => {
            const text = th.textContent.replace(' ↑', '').replace(' ↓', '').replace(' ↕', '');
            if (th.dataset.sort === this.sortColumn) {
                th.textContent = text + (this.sortDirection === 'asc' ? ' ↑' : ' ↓');
            } else {
                th.textContent = text + ' ↕';
            }
        });
    }

    renderTable() {
        const tbody = document.getElementById('tableBody');
        tbody.innerHTML = '';
        
        const start = (this.currentPage - 1) * this.pageSize;
        const end = Math.min(start + this.pageSize, this.filteredData.length);
        
        for (let i = start; i < end; i++) {
            const entry = this.filteredData[i];
            const row = tbody.insertRow();
            
            row.innerHTML = `
                <td>${entry.datetime}</td>
                <td class="rank-${this.getRankClass(entry.rank)}">${entry.rank}</td>
                <td>${entry.name}</td>
                <td class="points">${entry.pointsDisplay}</td>
                <td class="${this.getChangeClass(entry.diff1h)}">${entry.diff1h}</td>
                <td class="${this.getChangeClass(entry.diff3h)}">${entry.diff3h}</td>
                <td class="${this.getChangeClass(entry.diff6h)}">${entry.diff6h}</td>
                <td class="${this.getChangeClass(entry.diff9h)}">${entry.diff9h}</td>
                <td class="${this.getChangeClass(entry.diff12h)}">${entry.diff12h}</td>
                <td class="${this.getChangeClass(entry.diff15h)}">${entry.diff15h}</td>
                <td class="${this.getChangeClass(entry.diff18h)}">${entry.diff18h}</td>
                <td class="${this.getChangeClass(entry.diff21h)}">${entry.diff21h}</td>
                <td class="${this.getChangeClass(entry.diff24h)}">${entry.diff24h}</td>
                <td class="${this.getChangeClass(entry.diff36h)}">${entry.diff36h}</td>
                <td class="${this.getChangeClass(entry.diff48h)}">${entry.diff48h}</td>
                <td class="${this.getChangeClass(entry.diff60h)}">${entry.diff60h}</td>
                <td class="${this.getChangeClass(entry.diff72h)}">${entry.diff72h}</td>
                <td class="${this.getChangeClass(entry.diff84h)}">${entry.diff84h}</td>
                <td class="${this.getChangeClass(entry.diff96h)}">${entry.diff96h}</td>
                <td class="${this.getChangeClass(entry.diff108h)}">${entry.diff108h}</td>
                <td class="${this.getChangeClass(entry.diff120h)}">${entry.diff120h}</td>
                <td class="${this.getChangeClass(entry.diff132h)}">${entry.diff132h}</td>
                <td class="${this.getChangeClass(entry.diff144h)}">${entry.diff144h}</td>
                <td class="${this.getChangeClass(entry.diff156h)}">${entry.diff156h}</td>
                <td class="${this.getChangeClass(entry.diff168h)}">${entry.diff168h}</td>
                <td class="${this.getChangeClass(entry.diff180h)}">${entry.diff180h}</td>
            `;
        }
        
        this.renderPagination();
    }

    getRankClass(rank) {
        if (rank <= 3) return 'gold';
        if (rank <= 10) return 'silver';
        if (rank <= 50) return 'bronze';
        return 'normal';
    }

    getChangeClass(value) {
        if (typeof value === 'string') {
            if (value.startsWith('+')) return 'positive';
            if (value.startsWith('-') && value !== '-') return 'negative';
        }
        return '';
    }

    renderPagination() {
        const pagination = document.getElementById('pagination');
        pagination.innerHTML = '';
        
        if (this.pageSize >= this.filteredData.length) return;
        
        const totalPages = Math.ceil(this.filteredData.length / this.pageSize);
        
        const createButton = (text, page, disabled = false) => {
            const button = document.createElement('button');
            button.textContent = text;
            button.disabled = disabled;
            if (!disabled) {
                button.addEventListener('click', () => {
                    this.currentPage = page;
                    this.renderTable();
                });
            }
            return button;
        };
        
        pagination.appendChild(createButton('<<', 1, this.currentPage === 1));
        pagination.appendChild(createButton('<', this.currentPage - 1, this.currentPage === 1));
        
        const startPage = Math.max(1, this.currentPage - 2);
        const endPage = Math.min(totalPages, startPage + 4);
        
        for (let i = startPage; i <= endPage; i++) {
            const button = createButton(i.toString(), i);
            if (i === this.currentPage) {
                button.classList.add('active');
            }
            pagination.appendChild(button);
        }
        
        pagination.appendChild(createButton('>', this.currentPage + 1, this.currentPage === totalPages));
        pagination.appendChild(createButton('>>', totalPages, this.currentPage === totalPages));
        
        const pageInfo = document.createElement('span');
        pageInfo.className = 'page-info';
        pageInfo.textContent = `${this.currentPage} / ${totalPages} ページ`;
        pagination.appendChild(pageInfo);
    }

    updateStats() {
        document.getElementById('totalRecords').textContent = this.allData.length.toLocaleString();
        document.getElementById('displayedRecords').textContent = this.filteredData.length.toLocaleString();
        
        if (this.allData.length > 0) {
            const latestEntry = this.allData.reduce((latest, entry) => {
                return entry.datetime > latest.datetime ? entry : latest;
            });
            document.getElementById('lastUpdate').textContent = latestEntry.datetime;
        }
        
        const changes = this.filteredData
            .map(e => parseInt(e.diff24h.toString().replace(/[^-\d]/g, '')) || 0)
            .filter(v => v !== 0);
        
        if (changes.length > 0) {
            const maxGain = Math.max(...changes);
            const maxLoss = Math.min(...changes);
            const avgChange = Math.round(changes.reduce((a, b) => a + b, 0) / changes.length);
            
            document.getElementById('maxGain').textContent = maxGain > 0 ? `+${maxGain.toLocaleString()}` : '-';
            document.getElementById('maxLoss').textContent = maxLoss < 0 ? maxLoss.toLocaleString() : '-';
            document.getElementById('avgChange').textContent = avgChange !== 0 ? 
                (avgChange > 0 ? `+${avgChange.toLocaleString()}` : avgChange.toLocaleString()) : '-';
        }
    }

    exportData() {
        const headers = ['日時', '順位', 'プレイヤー名', 'ポイント', '1h', '3h', '6h', '9h', '12h', '15h', '18h', '21h', '24h', '36h(1.5d)', '48h(2d)', '60h(2.5d)', '72h(3d)', '84h(3.5d)', '96h(4d)', '108h(4.5d)', '120h(5d)', '132h(5.5d)', '144h(6d)', '156h(6.5d)', '168h(7d)', '180h(7.5d)'];
        const rows = this.filteredData.map(entry => [
            entry.datetime,
            entry.rank,
            entry.name,
            entry.pointsDisplay,
            entry.diff1h,
            entry.diff3h,
            entry.diff6h,
            entry.diff9h,
            entry.diff12h,
            entry.diff15h,
            entry.diff18h,
            entry.diff21h,
            entry.diff24h,
            entry.diff36h,
            entry.diff48h,
            entry.diff60h,
            entry.diff72h,
            entry.diff84h,
            entry.diff96h,
            entry.diff108h,
            entry.diff120h,
            entry.diff132h,
            entry.diff144h,
            entry.diff156h,
            entry.diff168h,
            entry.diff180h
        ]);
        
        const csvContent = [headers, ...rows]
            .map(row => row.map(cell => `"${cell}"`).join(','))
            .join('\n');
        
        const blob = new Blob(['\ufeff' + csvContent], { type: 'text/csv;charset=utf-8;' });
        const link = document.createElement('a');
        const url = URL.createObjectURL(blob);
        link.setAttribute('href', url);
        link.setAttribute('download', `ranking_export_${new Date().toISOString().slice(0, 10)}.csv`);
        link.style.visibility = 'hidden';
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
    }

    showLoading(show) {
        const spinner = document.getElementById('loadingSpinner');
        if (show) {
            spinner.classList.remove('hidden');
        } else {
            spinner.classList.add('hidden');
        }
    }
}

document.addEventListener('DOMContentLoaded', () => {
    new RankingViewer();
});