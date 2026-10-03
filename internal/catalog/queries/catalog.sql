-- name: ListProducts :many
SELECT Id, Name, Brand, Category FROM Product ORDER BY Id;

-- name: InsertProduct :one
INSERT INTO Product (Name, Brand, Category) VALUES (?, ?, ?) RETURNING Id;

-- name: FindSellerLink :one
SELECT Id, SellerName, ProductId, SellerProductId FROM SellerProduct
WHERE SellerName = ? AND SellerProductId = ?;

-- name: InsertSellerLink :one
INSERT INTO SellerProduct (SellerName, ProductId, SellerProductId) VALUES (?, ?, ?) RETURNING Id;
