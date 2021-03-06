// Copyright (C) 2017, 2018, 2019 EGAAS S.A.
//
// This program is free software; you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation; either version 2 of the License, or (at
// your option) any later version.
//
// This program is distributed in the hope that it will be useful, but
// WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
// General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program; if not, write to the Free Software
// Foundation, Inc., 51 Franklin Street, Fifth Floor, Boston, MA 02110-1301, USA.

package rollback

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AplaProject/go-apla/packages/consts"
	"github.com/AplaProject/go-apla/packages/converter"
	"github.com/AplaProject/go-apla/packages/model"
	"github.com/AplaProject/go-apla/packages/smart"

	log "github.com/sirupsen/logrus"
)

func rollbackUpdatedRow(tx map[string]string, where string, dbTransaction *model.DbTransaction, logger *log.Entry) error {
	var rollbackInfo map[string]string
	if err := json.Unmarshal([]byte(tx["data"]), &rollbackInfo); err != nil {
		logger.WithFields(log.Fields{"type": consts.JSONUnmarshallError, "error": err}).Error("unmarshalling rollback.Data from json")
		return err
	}
	addSQLUpdate := ""
	for k, v := range rollbackInfo {
		if v == "NULL" {
			addSQLUpdate += k + `=NULL,`
		} else if converter.IsByteColumn(tx["table_name"], k) && len(v) != 0 {
			addSQLUpdate += k + `=decode('` + string(converter.BinToHex([]byte(v))) + `','HEX'),`
		} else {
			addSQLUpdate += k + `='` + strings.Replace(v, `'`, `''`, -1) + `',`
		}
	}
	addSQLUpdate = addSQLUpdate[0 : len(addSQLUpdate)-1]
	if err := model.Update(dbTransaction, tx["table_name"], addSQLUpdate, where); err != nil {
		logger.WithFields(log.Fields{"type": consts.JSONUnmarshallError, "error": err, "query": addSQLUpdate}).Error("updating table")
		return err
	}
	return nil
}

func rollbackInsertedRow(tx map[string]string, where string, dbTransaction *model.DbTransaction, logger *log.Entry) error {
	if err := model.Delete(dbTransaction, tx["table_name"], where); err != nil {
		logger.WithFields(log.Fields{"type": consts.DBError, "error": err}).Error("deleting from table")
		return err
	}
	return nil
}

func rollbackTransaction(txHash []byte, dbTransaction *model.DbTransaction, logger *log.Entry) error {
	rollbackTx := &model.RollbackTx{}
	txs, err := rollbackTx.GetRollbackTransactions(dbTransaction, txHash)
	if err != nil {
		logger.WithFields(log.Fields{"type": consts.DBError, "error": err}).Error("getting rollback transactions")
		return err
	}
	for _, tx := range txs {
		if tx["table_name"] == smart.SysName {
			var sysData smart.SysRollData
			err := json.Unmarshal([]byte(tx["data"]), &sysData)
			if err != nil {
				logger.WithFields(log.Fields{"type": consts.JSONUnmarshallError, "error": err}).Error("unmarshalling rollback.Data from json")
				return err
			}
			switch sysData.Type {
			case "NewTable":
				smart.SysRollbackTable(dbTransaction, sysData)
			case "NewColumn":
				smart.SysRollbackColumn(dbTransaction, sysData)
			case "NewContract":
				smart.SysRollbackNewContract(sysData, tx["table_id"])
			case "EditContract":
				smart.SysRollbackEditContract(dbTransaction, sysData, tx["table_id"])
			case "NewEcosystem":
				smart.SysRollbackEcosystem(dbTransaction, sysData)
			case "ActivateContract":
				smart.SysRollbackActivate(sysData)
			case "DeactivateContract":
				smart.SysRollbackDeactivate(sysData)
			case "DeleteColumn":
				smart.SysRollbackDeleteColumn(dbTransaction, sysData)
			case "DeleteTable":
				smart.SysRollbackDeleteTable(dbTransaction, sysData)
			}
			continue
		}
		where := " WHERE id='" + tx["table_id"] + `'`
		table := tx[`table_name`]
		if under := strings.IndexByte(table, '_'); under > 0 {
			keyName := table[under+1:]
			if v, ok := converter.FirstEcosystemTables[keyName]; ok && !v {
				where += fmt.Sprintf(` AND ecosystem='%d'`, converter.StrToInt64(table[:under]))
				tx[`table_name`] = `1_` + keyName
			}
		}
		if len(tx["data"]) > 0 {
			if err := rollbackUpdatedRow(tx, where, dbTransaction, logger); err != nil {
				return err
			}
		} else {
			if err := rollbackInsertedRow(tx, where, dbTransaction, logger); err != nil {
				return err
			}
		}
	}
	txForDelete := &model.RollbackTx{TxHash: txHash}
	err = txForDelete.DeleteByHash(dbTransaction)
	if err != nil {
		logger.WithFields(log.Fields{"type": consts.DBError, "error": err}).Error("deleting rollback transaction by hash")
		return err
	}
	return nil
}
